// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import "strings"

// scanString scans a quoted literal opened by `open` and closed by `close`,
// emitting begEv for the opener, on_tstring_content for the body (split at
// interpolation boundaries when interp is true) and on_tstring_end for the
// closer. A literal that meets end of input sets the Unterminated flag.
func (l *lexer) scanString(open, close byte, begEv Event, interp bool) {
	line, col := l.startPos()
	l.advance(1) // opener
	l.state = ExprBeg
	l.toks = append(l.toks, Token{Event: begEv, Tok: string(open), State: l.state, Line: line, Col: col})
	l.scanStringBody(close, begEv, interp, 0)
}

// scanStringBody reads the content + closer of a string-like literal. nesting,
// when nonzero, is the matching open delimiter for %-literals with paired
// delimiters so they can nest.
func (l *lexer) scanStringBody(close byte, begEv Event, interp bool, openDelim byte) {
	depth := 0
	cstart := l.pos
	cline, ccol := l.startPos()
	flush := func() {
		if l.pos > cstart {
			l.toks = append(l.toks, Token{Event: EvTstringContent, Tok: l.src[cstart:l.pos], State: l.state, Line: cline, Col: ccol})
		}
	}
	for !l.eof() {
		c := l.peek()
		switch {
		case c == '\\':
			l.advance(2)
			continue
		case openDelim != 0 && c == openDelim:
			depth++
			l.advance(1)
			continue
		case c == close:
			if depth > 0 {
				depth--
				l.advance(1)
				continue
			}
			flush()
			eline, ecol := l.startPos()
			estart := l.pos
			l.advance(1)
			if begEv == EvRegexpBeg {
				for !l.eof() && isRegexpFlag(l.peek()) {
					l.advance(1)
				}
				l.state = ExprEnd
				l.toks = append(l.toks, Token{Event: EvRegexpEnd, Tok: l.src[estart:l.pos], State: l.state, Line: eline, Col: ecol})
				return
			}
			l.state = ExprEnd
			l.toks = append(l.toks, Token{Event: EvTstringEnd, Tok: string(close), State: l.state, Line: eline, Col: ecol})
			return
		case interp && c == '#' && (l.peekAt(1) == '{' || l.peekAt(1) == '@' || l.peekAt(1) == '$'):
			flush()
			if l.peekAt(1) == '{' {
				l.scanEmbexpr()
			} else {
				l.scanEmbvar()
			}
			cstart = l.pos
			cline, ccol = l.startPos()
			continue
		default:
			l.advance(1)
		}
	}
	flush()
	l.unterminated = true
}

func isRegexpFlag(b byte) bool {
	switch b {
	case 'i', 'm', 'x', 'o', 'u', 'n', 'e', 's':
		return true
	}
	return false
}

// scanEmbexpr scans an interpolation #{ ... } as a nested expression, emitting
// on_embexpr_beg / inner tokens / on_embexpr_end.
func (l *lexer) scanEmbexpr() {
	line, col := l.startPos()
	l.advance(2) // #{
	prevState := l.state
	l.state = ExprBeg
	l.toks = append(l.toks, Token{Event: EvEmbexprBeg, Tok: "#{", State: l.state, Line: line, Col: col})
	// Nested braces are real on_lbrace/on_rbrace tokens (tracked by braceDepth);
	// the interpolation closes at the first `}` that has no matching open brace.
	baseBrace := l.braceDepth
	for !l.eof() {
		if l.peek() == '}' && l.braceDepth == baseBrace {
			eline, ecol := l.startPos()
			l.advance(1)
			l.state = ExprEnd
			l.toks = append(l.toks, Token{Event: EvEmbexprEnd, Tok: "}", State: l.state, Line: eline, Col: ecol})
			l.state = prevState
			return
		}
		l.scanToken()
	}
	l.state = prevState
	l.unterminated = true
}

// scanEmbvar scans a #@ivar / #$gvar interpolation.
func (l *lexer) scanEmbvar() {
	line, col := l.startPos()
	l.advance(1) // '#'
	l.toks = append(l.toks, Token{Event: EvEmbvar, Tok: "#", State: l.state, Line: line, Col: col})
	if l.peek() == '@' {
		l.scanAt()
	} else {
		l.scanGvar()
	}
}

// percentLiteralOK reports whether `%` begins a percent-literal here (value
// position) rather than the modulo operator.
func (l *lexer) percentLiteralOK() bool {
	if l.state&(ExprBeg|ExprMid|ExprLabel) != 0 {
		return true
	}
	if l.state&(ExprArg|ExprCmdArg) != 0 && l.spaceBefore() {
		n := l.peekAt(1)
		return n != ' ' && n != '\t' && n != '=' && n != 0
	}
	return false
}

var percentClose = map[byte]byte{'(': ')', '[': ']', '{': '}', '<': '>'}

// scanPercent scans a %-literal: %w %i %q %Q %r %s %x and the bare %(...).
func (l *lexer) scanPercent() {
	line, col := l.startPos()
	start := l.pos
	l.advance(1) // %
	typ := byte('Q')
	if isPercentType(l.peek()) {
		typ = l.peek()
		l.advance(1)
	}
	delim := l.peek()
	if delim == 0 || isSpace(delim) || isIdentCont(delim) {
		// not actually a percent literal (e.g. `% ` modulo) — treat the % as op.
		l.pos = start
		l.col = col
		l.scanOp()
		return
	}
	l.advance(1) // open delim
	open := delim
	close := delim
	if c, ok := percentClose[delim]; ok {
		close = c
	} else {
		open = 0 // non-paired delimiter cannot nest
	}
	begTok := l.src[start:l.pos]

	var begEv Event
	interp := true
	wordList := false
	switch typ {
	case 'w':
		begEv, interp, wordList = EvQwordsBeg, false, true
	case 'W':
		begEv, interp, wordList = EvWordsBeg, true, true
	case 'i':
		begEv, interp, wordList = EvQsymbolsBeg, false, true
	case 'I':
		begEv, interp, wordList = EvSymbolsBeg, true, true
	case 'q':
		begEv, interp = EvTstringBeg, false
	case 'r':
		begEv, interp = EvRegexpBeg, true
	case 's':
		begEv, interp = EvSymbeg, false
	case 'x':
		begEv, interp = EvBacktick, true
	default:
		// 'Q' and the bare `%(...)` form (typ defaults to 'Q'): a double-quoted,
		// interpolating string literal.
		begEv, interp = EvTstringBeg, true
	}
	l.state = ExprBeg
	l.toks = append(l.toks, Token{Event: begEv, Tok: begTok, State: l.state, Line: line, Col: col})
	if wordList {
		l.scanWordList(open, close, interp)
		return
	}
	l.scanStringBody(close, begEv, interp, open)
}

func isPercentType(b byte) bool {
	switch b {
	case 'w', 'W', 'i', 'I', 'q', 'Q', 'r', 's', 'x':
		return true
	}
	return false
}

// scanWordList scans the body of a %w/%i/%W/%I list, separating words with
// on_words_sep and ending with on_tstring_end.
func (l *lexer) scanWordList(open, close byte, interp bool) {
	depth := 0
	// leading separators
	for {
		// separator run
		sstart := l.pos
		sline, scol := l.startPos()
		for !l.eof() && isSpace(l.peek()) {
			l.advance(1)
		}
		if l.pos > sstart {
			l.toks = append(l.toks, Token{Event: EvWordsSep, Tok: l.src[sstart:l.pos], State: l.state, Line: sline, Col: scol})
		}
		if l.eof() {
			l.unterminated = true
			return
		}
		if l.peek() == close && depth == 0 {
			eline, ecol := l.startPos()
			l.advance(1)
			l.state = ExprEnd
			l.toks = append(l.toks, Token{Event: EvTstringEnd, Tok: string(close), State: l.state, Line: eline, Col: ecol})
			return
		}
		// a word
		wstart := l.pos
		wline, wcol := l.startPos()
		for !l.eof() {
			c := l.peek()
			if c == '\\' {
				l.advance(2)
				continue
			}
			if open != 0 && c == open {
				depth++
				l.advance(1)
				continue
			}
			if c == close {
				if depth > 0 {
					depth--
					l.advance(1)
					continue
				}
				break
			}
			if isSpace(c) || c == '\n' {
				break
			}
			l.advance(1)
		}
		if l.pos > wstart {
			// Ripper retags the final word of an unterminated word-list as
			// on_tstring_end, which balances the nesting stack (opens becomes
			// empty) while the recoverable compile error still drives "more
			// input". We reproduce that so indent/ltype match IRB exactly.
			ev := EvTstringContent
			if l.eof() {
				ev = EvTstringEnd
				l.state = ExprEnd
			}
			l.toks = append(l.toks, Token{Event: ev, Tok: l.src[wstart:l.pos], State: l.state, Line: wline, Col: wcol})
		}
		if l.eof() {
			l.unterminated = true
			return
		}
	}
}

func (l *lexer) scanRegexp() {
	line, col := l.startPos()
	l.advance(1) // '/'
	l.state = ExprBeg
	l.toks = append(l.toks, Token{Event: EvRegexpBeg, Tok: "/", State: l.state, Line: line, Col: col})
	l.scanRegexpBody()
}

// scanRegexpBody reads regexp content + closing / + flags.
func (l *lexer) scanRegexpBody() {
	cstart := l.pos
	cline, ccol := l.startPos()
	flush := func() {
		if l.pos > cstart {
			l.toks = append(l.toks, Token{Event: EvTstringContent, Tok: l.src[cstart:l.pos], State: l.state, Line: cline, Col: ccol})
		}
	}
	for !l.eof() {
		c := l.peek()
		switch {
		case c == '\\':
			l.advance(2)
		case c == '/':
			flush()
			eline, ecol := l.startPos()
			estart := l.pos
			l.advance(1)
			for !l.eof() && isRegexpFlag(l.peek()) {
				l.advance(1)
			}
			l.state = ExprEnd
			l.toks = append(l.toks, Token{Event: EvRegexpEnd, Tok: l.src[estart:l.pos], State: l.state, Line: eline, Col: ecol})
			return
		case c == '#' && (l.peekAt(1) == '{' || l.peekAt(1) == '@' || l.peekAt(1) == '$'):
			flush()
			if l.peekAt(1) == '{' {
				l.scanEmbexpr()
			} else {
				l.scanEmbvar()
			}
			cstart = l.pos
			cline, ccol = l.startPos()
		default:
			l.advance(1)
		}
	}
	flush()
	l.unterminated = true
}

func (l *lexer) heredocOK() bool {
	// `<<` is a heredoc when in value position and followed by ~,-,",',` or an
	// identifier/uppercase letter.
	if l.state&(ExprBeg|ExprMid|ExprLabel) == 0 {
		if l.state&(ExprArg|ExprCmdArg) == 0 || !l.spaceBefore() {
			return false
		}
	}
	c := l.peekAt(2)
	if c == '~' || c == '-' {
		c2 := l.peekAt(3)
		return c2 == '"' || c2 == '\'' || c2 == '`' || isIdentStart(c2)
	}
	return c == '"' || c == '\'' || c == '`' || isUpper(c)
}

// scanHeredocBeg recognises the `<<ID` opener, emits on_heredoc_beg and queues
// the body to be read when the current line's newline arrives. Returns false if
// it turns out not to be a heredoc.
func (l *lexer) scanHeredocBeg() bool {
	line, col := l.startPos()
	start := l.pos
	// heredocOK has already vetted the lookahead (a dash/tilde is followed by a
	// quote or identifier; a bare `<<` is followed by a quote or uppercase
	// letter), so the delimiter is always present here.
	p := l.pos + 2 // skip <<
	h := &heredoc{}
	if l.src[p] == '~' || l.src[p] == '-' {
		h.dashed = true
		p++
	}
	var id string
	switch l.src[p] {
	case '"', '\'', '`':
		q := l.src[p]
		p++
		idStart := p
		for p < len(l.src) && l.src[p] != q {
			p++
		}
		if p >= len(l.src) {
			// quoted delimiter that never closes — not a heredoc opener.
			return false
		}
		id = l.src[idStart:p]
		p++ // closing quote
	default:
		idStart := p
		for p < len(l.src) && isIdentCont(l.src[p]) {
			p++
		}
		id = l.src[idStart:p]
	}
	h.id = id
	// advance over the opener text
	n := p - l.pos
	l.advance(n)
	l.state = ExprEnd
	l.toks = append(l.toks, Token{Event: EvHeredocBeg, Tok: l.src[start:l.pos], State: l.state, Line: line, Col: col})
	l.pendingHeredocs = append(l.pendingHeredocs, h)
	return true
}

// readHeredocBody consumes the body lines of a queued heredoc up to its
// terminator, emitting on_tstring_content for the body and on_heredoc_end for
// the terminator line. A body that meets end of input increments openHeredocs.
func (l *lexer) readHeredocBody(h *heredoc) {
	for !l.eof() {
		lineStart := l.pos
		lline, lcol := l.startPos()
		// read one physical line including newline
		for !l.eof() && l.peek() != '\n' {
			l.advance(1)
		}
		hadNL := false
		if !l.eof() {
			l.advance(1)
			hadNL = true
		}
		raw := l.src[lineStart:l.pos]
		trimmed := raw
		if hadNL {
			trimmed = raw[:len(raw)-1]
		}
		check := trimmed
		if h.dashed {
			check = strings.TrimLeft(check, " \t")
		}
		if check == h.id {
			l.state = ExprEnd
			l.toks = append(l.toks, Token{Event: EvHeredocEnd, Tok: trimmed, State: l.state, Line: lline, Col: lcol})
			// The newline following the terminator is a separate on_nl, kept so
			// the joined token text reconstructs the source exactly.
			if hadNL {
				l.toks = append(l.toks, Token{Event: EvNl, Tok: "\n", State: ExprBeg, Line: lline, Col: lcol + len(trimmed)})
			}
			return
		}
		// body line (content); interpolation inside is not separately tokenised
		// here — IRB's continuation/indent logic only needs the heredoc open/close
		// structure, and colorize treats heredoc bodies as plain content.
		l.toks = append(l.toks, Token{Event: EvTstringContent, Tok: raw, State: ExprBeg, Line: lline, Col: lcol})
	}
	l.openHeredocs++
}
