// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import "strings"

// Lex tokenizes Ruby source into a Ripper-faithful token stream (events +
// lexer states + positions). It is a hand-written scanner covering the surface
// IRB's RubyLex / NestingParser / Color actually inspect: keywords, identifiers,
// the four variable sigils, constants, the full numeric grammar, operators,
// string / regexp / symbol / word-list literals (including %-literals),
// heredocs, comments, =begin/=end embdocs, string interpolation and labels.
//
// It deliberately does not build an AST; like Ripper.lex it returns a flat list
// of tokens whose joined Tok fields reconstruct the input exactly. Unterminated
// literals are reported through the Unterminated flag rather than as an error so
// IRB's "needs more input" decision can use them.
type lexResult struct {
	tokens       []Token
	unterminated bool // an open string/regexp/word-list/symbol met end of input
	openHeredocs int  // heredocs whose body never closed
}

type lexer struct {
	src   string
	pos   int
	line  int
	col   int // byte column within current line (0-based)
	state int
	toks  []Token

	// pending heredocs whose body has not yet been consumed; bodies are read
	// when the newline that ends the opening line is reached.
	pendingHeredocs []*heredoc

	unterminated bool
	openHeredocs int

	// lambdaHeads counts pending `->` lambda heads whose body brace has not yet
	// appeared, so a following `{` is recognised as on_tlambeg, not on_lbrace.
	lambdaHeads int

	// braceDepth counts currently-open `{`/tlambeg braces, so a `}` with no
	// matching opener can be lexed as on_embexpr_end (Ripper's behaviour).
	braceDepth int
}

type heredoc struct {
	id     string // delimiter identifier
	dashed bool   // <<- or <<~ (indented terminator allowed)
}

// Lex runs the scanner over code and returns the recognised tokens.
func Lex(code string) []Token {
	return lexInternal(code).tokens
}

func lexInternal(code string) lexResult {
	l := &lexer{src: code, line: 1, state: ExprBeg}
	l.run()
	return lexResult{
		tokens:       l.toks,
		unterminated: l.unterminated,
		openHeredocs: l.openHeredocs,
	}
}

func (l *lexer) peek() byte {
	if l.pos < len(l.src) {
		return l.src[l.pos]
	}
	return 0
}

func (l *lexer) peekAt(n int) byte {
	if l.pos+n < len(l.src) {
		return l.src[l.pos+n]
	}
	return 0
}

func (l *lexer) eof() bool { return l.pos >= len(l.src) }

func (l *lexer) run() {
	for !l.eof() {
		l.scanToken()
	}
	// Any heredocs that were opened but whose body never appeared (input ended
	// on the opening line) count as open.
	l.openHeredocs += len(l.pendingHeredocs)
}

func (l *lexer) startPos() (int, int) { return l.line, l.col }

// advance consumes n bytes from src, updating line/col bookkeeping.
func (l *lexer) advance(n int) {
	for i := 0; i < n && l.pos < len(l.src); i++ {
		if l.src[l.pos] == '\n' {
			l.line++
			l.col = 0
		} else {
			l.col++
		}
		l.pos++
	}
}

func (l *lexer) scanToken() {
	c := l.peek()
	switch {
	case c == '\n':
		l.scanNewline()
	case c == '\\' && l.peekAt(1) == '\n':
		// An explicit line continuation outside a literal: emit it as the on_sp
		// token "\\\n" that should_continue? recognises.
		line, col := l.startPos()
		l.advance(2)
		l.toks = append(l.toks, Token{Event: EvSp, Tok: "\\\n", State: l.state, Line: line, Col: col})
	case isSpace(c):
		l.scanSpace()
	case c == '#':
		l.scanComment()
	case c == '=' && l.col == 0 && l.hasPrefix("=begin") && embdocBoundary(l.src, l.pos+len("=begin")):
		l.scanEmbdoc()
	case isDigit(c):
		l.scanNumber()
	case c == '@':
		l.scanAt()
	case c == '$':
		l.scanGvar()
	case c == '"':
		l.scanString('"', '"', EvTstringBeg, true)
	case c == '\'':
		l.scanString('\'', '\'', EvTstringBeg, false)
	case c == '`':
		l.scanBacktick()
	case c == ':':
		l.scanColon()
	case c == '%' && l.percentLiteralOK():
		l.scanPercent()
	case c == '/' && l.regexpOK():
		l.scanRegexp()
	case c == '?' && l.charLiteralOK():
		l.scanCharLiteral()
	case c == '<' && l.peekAt(1) == '<' && l.heredocOK():
		if !l.scanHeredocBeg() {
			l.scanOp()
		}
	case isIdentStart(c):
		l.scanWord()
	default:
		l.scanPunct(c)
	}
}

func (l *lexer) hasPrefix(s string) bool {
	return strings.HasPrefix(l.src[l.pos:], s)
}

// embdocBoundary reports whether =begin is followed by EOL/space/EOF.
func embdocBoundary(src string, idx int) bool {
	if idx >= len(src) {
		return true
	}
	return src[idx] == '\n' || src[idx] == ' ' || src[idx] == '\t' || src[idx] == '\r'
}

func (l *lexer) scanNewline() {
	line, col := l.startPos()
	// A newline at EXPR_BEG / after an operator / a comma is "ignored" (the
	// statement continues); otherwise it terminates the statement (on_nl). A
	// trailing comma always leaves the lexer in EXPR_BEG, so the bit test alone
	// captures the "line continues after a comma" case.
	ignored := l.state&(ExprBeg|ExprFname|ExprDot|ExprClass) != 0
	l.advance(1)
	prevState := l.state
	if ignored {
		l.state = prevState
		l.toks = append(l.toks, Token{Event: EvIgnoredNl, Tok: "\n", State: prevState, Line: line, Col: col})
	} else {
		l.toks = append(l.toks, Token{Event: EvNl, Tok: "\n", State: prevState, Line: line, Col: col})
		l.state = ExprBeg
	}
	l.consumePendingHeredocs()
}

func (l *lexer) consumePendingHeredocs() {
	if len(l.pendingHeredocs) == 0 {
		return
	}
	hs := l.pendingHeredocs
	l.pendingHeredocs = nil
	for _, h := range hs {
		l.readHeredocBody(h)
	}
}

func (l *lexer) scanSpace() {
	line, col := l.startPos()
	start := l.pos
	for !l.eof() && isSpace(l.peek()) {
		l.advance(1)
	}
	l.toks = append(l.toks, Token{Event: EvSp, Tok: l.src[start:l.pos], State: l.state, Line: line, Col: col})
	// A following backslash-newline is emitted by scanToken as its own on_sp
	// token "\\\n", which should_continue? keys on to force a line continuation.
}

func (l *lexer) scanComment() {
	line, col := l.startPos()
	start := l.pos
	for !l.eof() && l.peek() != '\n' {
		l.advance(1)
	}
	l.toks = append(l.toks, Token{Event: EvComment, Tok: l.src[start:l.pos], State: l.state, Line: line, Col: col})
}

func (l *lexer) scanEmbdoc() {
	// =begin ... =end block comment.
	line, col := l.startPos()
	// emit on_embdoc_beg (the =begin line)
	begStart := l.pos
	for !l.eof() && l.peek() != '\n' {
		l.advance(1)
	}
	if !l.eof() {
		l.advance(1) // include newline in beg token
	}
	l.toks = append(l.toks, Token{Event: EvEmbdocBeg, Tok: l.src[begStart:l.pos], State: l.state, Line: line, Col: col})
	for !l.eof() {
		lstart := l.pos
		eline, ecol := l.startPos()
		for !l.eof() && l.peek() != '\n' {
			l.advance(1)
		}
		if !l.eof() {
			l.advance(1)
		}
		seg := l.src[lstart:l.pos]
		if strings.HasPrefix(seg, "=end") && (len(seg) == 4 || seg[4] == '\n' || seg[4] == ' ' || seg[4] == '\t' || seg[4] == '\r') {
			l.toks = append(l.toks, Token{Event: EvEmbdocEnd, Tok: seg, State: l.state, Line: eline, Col: ecol})
			return
		}
		l.toks = append(l.toks, Token{Event: EvEmbdoc, Tok: seg, State: l.state, Line: eline, Col: ecol})
	}
	// =begin without =end: treat as still open (unterminated comment continues).
	l.unterminated = true
}

func (l *lexer) scanNumber() {
	line, col := l.startPos()
	start := l.pos
	ev := EvInt
	if l.peek() == '0' && (l.peekAt(1) == 'x' || l.peekAt(1) == 'X' ||
		l.peekAt(1) == 'b' || l.peekAt(1) == 'B' || l.peekAt(1) == 'o' ||
		l.peekAt(1) == 'O' || l.peekAt(1) == 'd' || l.peekAt(1) == 'D') {
		l.advance(2)
		for !l.eof() && (isHexDigit(l.peek()) || l.peek() == '_') {
			l.advance(1)
		}
	} else {
		for !l.eof() && (isDigit(l.peek()) || l.peek() == '_') {
			l.advance(1)
		}
		if l.peek() == '.' && isDigit(l.peekAt(1)) {
			ev = EvFloat
			l.advance(1)
			for !l.eof() && (isDigit(l.peek()) || l.peek() == '_') {
				l.advance(1)
			}
		}
		if l.peek() == 'e' || l.peek() == 'E' {
			save := l.pos
			l.advance(1)
			if l.peek() == '+' || l.peek() == '-' {
				l.advance(1)
			}
			if isDigit(l.peek()) {
				ev = EvFloat
				for !l.eof() && (isDigit(l.peek()) || l.peek() == '_') {
					l.advance(1)
				}
			} else {
				l.pos = save // not an exponent; rewind
			}
		}
	}
	// rational / imaginary suffixes
	if l.peek() == 'r' {
		l.advance(1)
		ev = EvRational
	}
	if l.peek() == 'i' {
		l.advance(1)
		ev = EvImaginary
	}
	l.state = ExprEnd
	l.toks = append(l.toks, Token{Event: ev, Tok: l.src[start:l.pos], State: l.state, Line: line, Col: col})
}

func isHexDigit(b byte) bool {
	return isDigit(b) || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func (l *lexer) scanAt() {
	line, col := l.startPos()
	start := l.pos
	l.advance(1)
	ev := EvIvar
	if l.peek() == '@' {
		l.advance(1)
		ev = EvCvar
	}
	for !l.eof() && isIdentCont(l.peek()) {
		l.advance(1)
	}
	l.state = ExprEnd
	l.toks = append(l.toks, Token{Event: ev, Tok: l.src[start:l.pos], State: l.state, Line: line, Col: col})
}

func (l *lexer) scanGvar() {
	line, col := l.startPos()
	start := l.pos
	l.advance(1)
	ev := EvGvar
	c := l.peek()
	switch {
	case isDigit(c):
		ev = EvBackref
		for !l.eof() && isDigit(l.peek()) {
			l.advance(1)
		}
	case c == '&' || c == '`' || c == '\'' || c == '+' || c == '~' || c == '!' ||
		c == '@' || c == '/' || c == '\\' || c == ';' || c == ',' || c == '.' ||
		c == '<' || c == '>' || c == '*' || c == '$' || c == '?' || c == ':' ||
		c == '"' || c == '0':
		if c == '&' || c == '`' || c == '\'' || c == '+' {
			ev = EvBackref
		}
		l.advance(1)
	default:
		for !l.eof() && isIdentCont(l.peek()) {
			l.advance(1)
		}
	}
	l.state = ExprEnd
	l.toks = append(l.toks, Token{Event: ev, Tok: l.src[start:l.pos], State: l.state, Line: line, Col: col})
}

func (l *lexer) scanWord() {
	line, col := l.startPos()
	start := l.pos
	for !l.eof() && isIdentCont(l.peek()) {
		l.advance(1)
	}
	// trailing ? or ! on method names (e.g. empty?, save!), but not when it is
	// the ternary `?` (only join when directly followed by no second char issue)
	if (l.peek() == '?' || l.peek() == '!') && l.peekAt(1) != '=' {
		l.advance(1)
	}
	word := l.src[start:l.pos]

	// label:  ident:  (not ::, and only in value position)
	if l.peek() == ':' && l.peekAt(1) != ':' && l.labelOK() {
		l.advance(1) // consume ':'
		prev := l.state
		l.state = ExprBeg | ExprLabel
		_ = prev
		l.toks = append(l.toks, Token{Event: EvLabel, Tok: word + ":", State: ExprArg | ExprLabeled, Line: line, Col: col})
		return
	}

	if reservedWords[word] && !l.kwIsMethodName() {
		l.emitKeyword(word, line, col)
		return
	}
	// Right after `def`, the method-name identifier is left in EXPR_ENDFN; the
	// colorizer paints such idents blue/bold. After a `.`/`::` the called method
	// is EXPR_ARG (not coloured), so only the def-name (ExprFname) gets ENDFN.
	if l.state&ExprFname != 0 {
		l.state = ExprEndFn
		ev := EvIdent
		if isUpper(word[0]) {
			ev = EvConst
		}
		l.toks = append(l.toks, Token{Event: ev, Tok: word, State: l.state, Line: line, Col: col})
		return
	}
	if l.state&ExprDot != 0 {
		l.state = ExprArg
		ev := EvIdent
		if isUpper(word[0]) {
			ev = EvConst
		}
		l.toks = append(l.toks, Token{Event: ev, Tok: word, State: l.state, Line: line, Col: col})
		return
	}
	if isUpper(word[0]) {
		l.state = ExprCmdArg
		l.toks = append(l.toks, Token{Event: EvConst, Tok: word, State: l.state, Line: line, Col: col})
		return
	}
	l.state = ExprCmdArg
	l.toks = append(l.toks, Token{Event: EvIdent, Tok: word, State: l.state, Line: line, Col: col})
}

// kwIsMethodName reports whether an identifier that matches a keyword is in fact
// a method name (preceded by `.` or `::` or `def`), in which case Ripper emits
// on_ident, not on_kw.
func (l *lexer) kwIsMethodName() bool {
	for i := len(l.toks) - 1; i >= 0; i-- {
		t := l.toks[i]
		if t.Event == EvSp || t.Event == EvComment {
			continue
		}
		return t.Event == EvPeriod || (t.Event == EvOp && t.Tok == "::")
	}
	return false
}

func (l *lexer) emitKeyword(word string, line, col int) {
	// A value-producing prior state means if/unless/while/until is a *modifier*
	// (Ripper marks it EXPR_LABEL), e.g. `foo if bar`; an EXPR_BEG prior state
	// means it opens a block, e.g. `if bar`.
	valueProduced := l.state&(ExprEnd|ExprArg|ExprCmdArg|ExprEndArg|ExprEndFn) != 0
	if word == "do" && l.lambdaHeads > 0 {
		l.lambdaHeads--
	}
	switch word {
	case "def":
		l.state = ExprFname
	case "class", "module":
		l.state = ExprClass
	case "end", "self", "true", "false", "nil", "__FILE__", "__LINE__", "__ENCODING__", "super", "yield", "retry", "redo", "break", "next", "return", "defined?":
		if word == "return" || word == "break" || word == "next" || word == "yield" || word == "super" || word == "defined?" {
			l.state = ExprMid
		} else {
			l.state = ExprEnd
		}
	default:
		l.state = ExprBeg
	}
	st := l.state
	// if/unless/while/until in modifier position keep EXPR_LABEL so the nesting
	// parser treats them as modifiers, not block openers.
	if (word == "if" || word == "unless" || word == "while" || word == "until") && valueProduced {
		st |= ExprLabel
	}
	l.toks = append(l.toks, Token{Event: EvKw, Tok: word, State: st, Line: line, Col: col})
}

// labelOK reports whether the lexer is in a position where a value (and thus a
// label or beginning-of-expression keyword) may start.
func (l *lexer) labelOK() bool {
	return l.state&(ExprBeg|ExprMid|ExprArg|ExprCmdArg|ExprFname|ExprDot|ExprClass|ExprLabel) != 0
}

func (l *lexer) scanColon() {
	line, col := l.startPos()
	if l.peekAt(1) == ':' {
		l.advance(2)
		l.state = ExprDot
		l.toks = append(l.toks, Token{Event: EvOp, Tok: "::", State: l.state, Line: line, Col: col})
		return
	}
	// symbol literal :foo / :"..." / :+ etc — only in value position.
	if l.symbolOK() {
		next := l.peekAt(1)
		if next == '"' || next == '\'' {
			// Ripper folds the opening quote into the symbeg token (`:"`), so the
			// whole quoted symbol colourises as one yellow run.
			l.advance(2) // ':' and the quote
			l.state = ExprBeg
			l.toks = append(l.toks, Token{Event: EvSymbeg, Tok: ":" + string(next), State: l.state, Line: line, Col: col})
			l.scanStringBody(next, EvTstringBeg, next == '"', 0)
			return
		}
		if isIdentStart(next) || isOpSymChar(next) {
			l.advance(1) // ':'
			l.toks = append(l.toks, Token{Event: EvSymbeg, Tok: ":", State: ExprFname, Line: line, Col: col})
			l.scanSymbolName(next)
			return
		}
		// A `:` with nothing after it (end of input) in value position is an
		// unterminated symbol beginning (Ripper emits on_symbeg), driving the
		// `:` ltype continuation prompt.
		if next == 0 {
			l.advance(1)
			l.state = ExprFname
			l.toks = append(l.toks, Token{Event: EvSymbeg, Tok: ":", State: l.state, Line: line, Col: col})
			l.unterminated = true
			return
		}
	}
	// bare colon (ternary else, label-end handled elsewhere)
	l.advance(1)
	l.state = ExprBeg
	l.toks = append(l.toks, Token{Event: EvOp, Tok: ":", State: l.state, Line: line, Col: col})
}

func isOpSymChar(b byte) bool {
	switch b {
	case '+', '-', '*', '/', '%', '<', '>', '=', '!', '~', '&', '|', '^', '[', '@':
		return true
	}
	return false
}

func (l *lexer) scanSymbolName(next byte) {
	line, col := l.startPos()
	start := l.pos
	if isIdentStart(next) {
		for !l.eof() && isIdentCont(l.peek()) {
			l.advance(1)
		}
		if l.peek() == '?' || l.peek() == '!' || l.peek() == '=' {
			l.advance(1)
		}
		l.state = ExprEnd
		l.toks = append(l.toks, Token{Event: EvIdent, Tok: l.src[start:l.pos], State: l.state, Line: line, Col: col})
		return
	}
	// operator symbol like :+ :[] :<=>
	for !l.eof() && isOpSymChar(l.peek()) {
		l.advance(1)
	}
	l.state = ExprEnd
	l.toks = append(l.toks, Token{Event: EvOp, Tok: l.src[start:l.pos], State: l.state, Line: line, Col: col})
}

func (l *lexer) symbolOK() bool {
	// Symbols start in value position; a `:` directly after a value is ternary.
	return l.state&(ExprBeg|ExprMid|ExprArg|ExprCmdArg|ExprFname|ExprLabel) != 0
}

func (l *lexer) scanBacktick() {
	if l.state&(ExprFname|ExprDot) != 0 {
		// `def \`` or method-name backtick
		line, col := l.startPos()
		l.advance(1)
		l.state = ExprArg
		l.toks = append(l.toks, Token{Event: EvBacktick, Tok: "`", State: l.state, Line: line, Col: col})
		return
	}
	l.scanString('`', '`', EvBacktick, true)
}

func (l *lexer) regexpOK() bool {
	return l.state&(ExprBeg|ExprMid|ExprLabel) != 0 ||
		(l.state&(ExprArg|ExprCmdArg) != 0 && l.spaceBefore() && !l.spaceAfterSlash())
}

// spaceBefore reports whether the previous token was whitespace. It is only ever
// called in ARG/CMDARG state, which always has a preceding token, so the token
// list is never empty here.
func (l *lexer) spaceBefore() bool {
	return l.toks[len(l.toks)-1].Event == EvSp
}

// spaceAfterSlash reports whether the `/` is followed by whitespace or `=`,
// which disambiguates a division operator (`a / b`, `a /= b`) from a regexp.
func (l *lexer) spaceAfterSlash() bool {
	c := l.peekAt(1)
	return c == ' ' || c == '\t' || c == '='
}

func (l *lexer) charLiteralOK() bool {
	if l.state&(ExprBeg|ExprMid|ExprLabel) == 0 {
		return false
	}
	c := l.peekAt(1)
	if c == 0 || c == ' ' || c == '\t' || c == '\n' {
		return false
	}
	// ?ab is not a char literal (ternary), but ?a + space is.
	if isIdentStart(c) && isIdentCont(l.peekAt(2)) {
		return false
	}
	return true
}

func (l *lexer) scanCharLiteral() {
	line, col := l.startPos()
	start := l.pos
	l.advance(1) // ?
	if l.peek() == '\\' {
		l.advance(2)
	} else {
		l.advance(1)
	}
	l.state = ExprEnd
	l.toks = append(l.toks, Token{Event: EvCHAR, Tok: l.src[start:l.pos], State: l.state, Line: line, Col: col})
}
