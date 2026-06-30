// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import (
	"strings"
	"testing"
)

// These tests exercise the remaining lexer / analysis branches — edge cases,
// EOF-in-literal paths and the indent special cases — that the corpus tests do
// not reach, holding the suite at 100% coverage without an interpreter.

func lexJoin(t *testing.T, s string) {
	t.Helper()
	var b strings.Builder
	for _, tk := range Lex(s) {
		b.WriteString(tk.Tok)
	}
	if b.String() != s {
		t.Errorf("round-trip %q -> %q", s, b.String())
	}
}

func TestLexerEdgeCases(t *testing.T) {
	// embvar interpolation in strings and regexps (#@ivar, #$gvar)
	lexJoin(t, `"value #@ivar and #$global"`)
	lexJoin(t, `/match #@x/`)
	// percent literals: every type and a non-paired delimiter
	for _, s := range []string{
		"%q{single}", "%Q{double #{x}}", "%r{regex}i", "%s{sym}", "%x{cmd}",
		"%W[A B]", "%I[a b]", "%(bare paren)", "%|piped|", "%w{nested {pair}}",
	} {
		lexJoin(t, s)
	}
	// percent that is actually modulo (space after %) and `a % b`
	lexJoin(t, "a % b")
	lexJoin(t, "x = 10 % 3")
	// char literals incl escapes, and ternary that is NOT a char literal
	lexJoin(t, "?A")
	lexJoin(t, "x ? y : z")
	// operators: compound assignment, spaceship, safe-nav, lambda arrow
	lexJoin(t, "a <=> b")
	lexJoin(t, "x **= 2")
	lexJoin(t, "obj&.m")
	lexJoin(t, "-> { }")
	// constant after dot and a constant method name in def
	lexJoin(t, "Foo::Bar.baz")
	lexJoin(t, "def Klass.method; end")
	// gvar special punctuation forms
	lexJoin(t, "$: $/ $\\ $; $, $. $< $> $* $$ $? $\"")
	// symbol with operator name and bracket
	lexJoin(t, ":[] :+ :<=>")
	// embdoc with no =end (unterminated) stays open
	if c, _ := CheckCode("=begin\nstuff"); c != More {
		t.Error("unterminated embdoc should be More")
	}
	// heredoc that never closes
	if c, _ := CheckCode("x = <<END\n  body never ends"); c != More {
		t.Error("unterminated heredoc should be More")
	}
	// embexpr that never closes inside a string
	if c, _ := CheckCode("\"#{1 + 2"); c != More {
		t.Error("unterminated embexpr should be More")
	}
}

func TestLexerLeadingDotMethodChain(t *testing.T) {
	// `a\n.b` — the ignored newline keeps the statement going.
	c, _ := CheckCode("a\n.b")
	if c != Complete {
		t.Errorf(".b chain: %v", c)
	}
}

func TestPercentModuloPositions(t *testing.T) {
	// `%` after a value with no space is modulo, not a literal.
	toks := Lex("a%b")
	for _, tk := range toks {
		if tk.Event == EvQwordsBeg || tk.Event == EvTstringBeg {
			t.Errorf("a%%b mislexed as percent literal: %v", tk)
		}
	}
}

func TestEmbdocBoundaryEOF(t *testing.T) {
	// =begin at EOF with nothing after it
	lexJoin(t, "=begin")
}

func TestScanSpaceBackslashContinuation(t *testing.T) {
	c, _ := CheckCode("1 +\\\n2")
	if c != Complete {
		t.Errorf("backslash continuation joined: %v", c)
	}
	// a bare trailing backslash-newline forces continuation
	c, _ = CheckCode("foo \\\n")
	if c != More {
		t.Errorf("trailing backslash: %v", c)
	}
}

func TestRegexpFlagsAndPercentR(t *testing.T) {
	lexJoin(t, "/abc/imxouesn")
	lexJoin(t, "%r{abc}im")
}

func TestNumberExponentRewind(t *testing.T) {
	// `1e` with no digits is `1` then `e` (ident), exercising the rewind path.
	lexJoin(t, "1e")
	lexJoin(t, "1end")
}

func TestHeredocIndentBranches(t *testing.T) {
	// squiggly heredoc: first content line, extra-indented content, close.
	src := "x = <<~SQL\n      SELECT 1\n    SQL"
	lines := strings.Split(src, "\n")
	// first line inside heredoc
	_ = AutoIndent(lines, 1, true)
	// inside content
	_ = AutoIndent(lines, 2, false)
	// at the close line
	_ = AutoIndent(lines, 2, true)

	// plain (non-dashed) heredoc takes the free-indent / zero path
	src2 := "y = <<END\nbody\nEND"
	lines2 := strings.Split(src2, "\n")
	_ = AutoIndent(lines2, 1, true)
	_ = AutoIndent(lines2, 1, false)

	// dashed heredoc
	src3 := "z = <<-END\n  body\n  END"
	lines3 := strings.Split(src3, "\n")
	_ = AutoIndent(lines3, 1, true)
}

func TestEmbdocIndent(t *testing.T) {
	src := "=begin\ndoc line\n=end"
	lines := strings.Split(src, "\n")
	_ = AutoIndent(lines, 1, true)
	_ = AutoIndent(lines, 2, true)
}

func TestFreeIndentInsideString(t *testing.T) {
	src := "x = \"first\n  second\n"
	lines := strings.Split(src, "\n")
	_ = AutoIndent(lines, 1, true)
	_ = AutoIndent(lines, 1, false)
	_ = AutoIndent(lines, 2, false)
}

func TestAutoIndentEmptyAndBeyond(t *testing.T) {
	// line index past the end (empty trailing line)
	_ = AutoIndent([]string{"x = 1", ""}, 5, true)
	// empty input
	_ = AutoIndent([]string{""}, 0, true)
}

func TestCalcIndentLevelSpecials(t *testing.T) {
	// alias / undef opens do not add indent
	_, opens := CheckCode("alias")
	_ = CalcIndentLevel(opens)
	// percent-string open adds indent (its tok starts with '%')
	_, popens := CheckCode("%Q{open")
	if CalcIndentLevel(popens) == 0 {
		t.Error("open %Q should indent")
	}
	// embdoc resets indent to 0
	_, eopens := CheckCode("=begin\nx")
	_ = CalcIndentLevel(eopens)
}

func TestLtypeNoLiteralOpen(t *testing.T) {
	// open block (def) has no literal type
	_, opens := CheckCode("def foo")
	if LtypeFromOpenTokens(opens) != "" {
		t.Error("block open should have empty ltype")
	}
}

func TestShouldContinueOperatorTail(t *testing.T) {
	for _, s := range []string{"a +", "a &&", "a ||", "a -", "a *", "a /", "a |", "a &"} {
		if c, _ := CheckCode(s); c != More {
			t.Errorf("trailing operator %q: %v", s, c)
		}
	}
	// endless range does NOT continue
	if c, _ := CheckCode("1.."); c != Complete {
		t.Errorf("endless range: %v", c)
	}
	// regexp / semicolon tails terminate
	if c, _ := CheckCode("/re/"); c != Complete {
		t.Error("regexp tail")
	}
	if c, _ := CheckCode("x;"); c != Complete {
		t.Error("semicolon tail")
	}
}

func TestPromptHelpersEdges(t *testing.T) {
	// rjust when string already wide enough
	if rjust("abc", 2) != "abc" {
		t.Error("rjust no-pad")
	}
	// lastLine of a single-line string returns it whole
	if lastLine("noNewline") != "noNewline" {
		t.Error("lastLine single")
	}
	if lastLine("a\nb") != "b" {
		t.Error("lastLine multi")
	}
}

func TestColorHelperEdges(t *testing.T) {
	// subClear on a line without a trailing newline
	if !strings.HasSuffix(subClear("abc", true), "\x1b[0m") {
		t.Error("subClear no-newline")
	}
	// clearSeq when not colorable
	if clearSeq(false) != "" {
		t.Error("clearSeq false")
	}
}

func TestSetOpStateScopeOp(t *testing.T) {
	// `::` leaves the lexer in EXPR_DOT (scope-resolution operator).
	toks := Lex("Foo::Bar")
	for _, tk := range toks {
		if tk.Event == EvOp && tk.Tok == "::" {
			if tk.State != ExprDot {
				t.Errorf(":: state = %d, want ExprDot", tk.State)
			}
		}
	}
}

func TestStepMethodHeadForms(t *testing.T) {
	// exercise the in_method_head state machine across receiver/name/arg forms
	for _, s := range []string{
		"def self.foo(a)\nend",
		"def obj.bar\nend",
		"def +(other)\nend",
		"def []=(k, v)\nend",
		"def name=(v)\nend",
		"def m a, b\nend",
		"def m(a, b) = a + b",
		"def @x.m; end",
	} {
		if c, _ := CheckCode(s); c == SyntaxError {
			t.Errorf("method head %q wrongly SyntaxError", s)
		}
	}
}

func TestStringPairedDelimNesting(t *testing.T) {
	// %(...) and %Q{...} with nested matching delimiters exercise the depth path.
	lexJoin(t, "%(a (b) c)")
	lexJoin(t, "%Q{outer {inner} done}")
	lexJoin(t, "%r{a {b} c}")
	// embvar interpolation inside a regexp body
	lexJoin(t, "/x #@y z/")
	lexJoin(t, "/x #$g z/")
	// %r terminated by its closing delimiter (regexp end via scanStringBody)
	lexJoin(t, "%r/abc/i")
}

func TestHeredocFalseStarts(t *testing.T) {
	// `<<` shapes that look heredoc-ish but are operators / incomplete.
	for _, s := range []string{
		"a << b",     // left shift, not heredoc
		"x = 1 << 2", // left shift
	} {
		lexJoin(t, s)
		if c, _ := CheckCode(s); c == SyntaxError {
			t.Errorf("%q wrongly SyntaxError", s)
		}
	}
	// quoted-delimiter heredocs
	lexJoin(t, "x = <<\"END\"\nbody\nEND")
	lexJoin(t, "x = <<`CMD`\nls\nCMD")
}

func TestScanRegexpEmbexpr(t *testing.T) {
	lexJoin(t, "/before #{x + y} after/")
}

func TestCharLiteralEscapes(t *testing.T) {
	lexJoin(t, "c = ?\\n")
	lexJoin(t, "c = ?\\t")
	// `?` followed by space is the ternary operator, not a char literal
	lexJoin(t, "a ? b : c")
}

func TestSymbolNameWithSuffix(t *testing.T) {
	lexJoin(t, ":empty?")
	lexJoin(t, ":danger!")
	lexJoin(t, ":name=")
}

func TestLtypeAllOpenForms(t *testing.T) {
	cases := map[string]string{
		"%(open":        `"`, // bare % → double-quote ltype
		"%Q(open":       `"`,
		"%q(open":       `'`,
		"x = `open":     "`", // backtick open
		"x = :\"open":   ":", // quoted symbol open
		"x = <<\"END\"": `"`, // double-quoted heredoc
		"x = <<'END'":   `'`, // single-quoted heredoc
		"x = <<`CMD`":   "`", // backtick heredoc
		"x = %w[a b":    "",  // word list closes (no ltype)
	}
	for code, want := range cases {
		_, opens := CheckCode(code)
		if got := LtypeFromOpenTokens(opens); got != want {
			t.Errorf("ltype(%q) = %q want %q (opens=%d)", code, got, want, len(opens))
		}
	}
}

func TestLeadingDotErrorNonDot(t *testing.T) {
	// first significant token is not a period → not a leading-dot error
	if c, _ := CheckCode("foo.bar"); c != Complete {
		t.Errorf("foo.bar: %v", c)
	}
	// leading whitespace then a period → still an error
	if c, _ := CheckCode("   ."); c != SyntaxError {
		t.Errorf("spaced leading dot: %v", c)
	}
}

func TestForWhileUntilDoCondition(t *testing.T) {
	// the in_for_while_until_condition `do` branch
	for _, s := range []string{
		"while x do\n  y\nend",
		"until x do\n  y\nend",
		"for i in a do\n  i\nend",
	} {
		if c, _ := CheckCode(s); c == SyntaxError {
			t.Errorf("%q wrongly SyntaxError", s)
		}
	}
}

func TestUnquotedSymbolInExpression(t *testing.T) {
	// `:sym` followed by more tokens exercises the in_unquoted_symbol pop branch
	lexJoin(t, "[:a, :b, :c]")
	lexJoin(t, "x = :foo.to_s")
}

func TestMethodHeadKeywordReceiver(t *testing.T) {
	// `def self.x` and `def nil?` style heads through the keyword-receiver path
	for _, s := range []string{
		"def true.x; end",
		"def false.y; end",
	} {
		if c, _ := CheckCode(s); c == SyntaxError {
			t.Errorf("%q wrongly SyntaxError", s)
		}
	}
}

func TestPromptPercentWidth(t *testing.T) {
	// `%3%` — a percent spec with a width produces empty (Ruby's gsub yields nil)
	ctx := PromptContext{IRBName: "irb"}
	if got := FormatPrompt("%3%", ctx, "", 0, 1); got != "" {
		t.Errorf("%%3%% = %q want empty", got)
	}
}

func TestRegexpAfterMethodArg(t *testing.T) {
	// `method /regex/` — `/` after an ident+space in arg position starts a regexp
	// (exercises regexpOK's arg-position clause and spaceBefore).
	toks := Lex("puts /abc/")
	foundRe := false
	for _, tk := range toks {
		if tk.Event == EvRegexpBeg {
			foundRe = true
		}
	}
	if !foundRe {
		t.Error("expected regexp after command arg")
	}
	// `a / b` (spaced both sides) is division, not a regexp.
	for _, tk := range Lex("a / b") {
		if tk.Event == EvRegexpBeg {
			t.Error("a / b mislexed as regexp")
		}
	}
}

func TestPercentAfterArgSpace(t *testing.T) {
	// `foo %w[a b]` — percent literal as a command argument.
	toks := Lex("foo %w[a b]")
	found := false
	for _, tk := range toks {
		if tk.Event == EvQwordsBeg {
			found = true
		}
	}
	if !found {
		t.Error("expected %w as command arg")
	}
}

func TestCharLiteralAfterMethodArg(t *testing.T) {
	lexJoin(t, "push ?x")
}

func TestMultilineColorPerLineClear(t *testing.T) {
	got := ColorizeCode("x = 1\ny = 2", true, true)
	if !strings.Contains(got, "\n") {
		t.Error("multiline colorize lost newline")
	}
}

func TestLambdaWithDoBody(t *testing.T) {
	// `-> do ... end` — the lambda head closed by `do` rather than `{`.
	if c, _ := CheckCode("-> do\n  x\nend"); c == SyntaxError {
		t.Error("lambda-do wrongly SyntaxError")
	}
}

func TestHeredocBeginFalseReturns(t *testing.T) {
	// `<<` at various truncations that are not valid heredoc openers fall back to
	// the shift operator.
	lexJoin(t, "x <<")
	lexJoin(t, "x << 5")
}

func TestStringEscapesAndNestedEmbexpr(t *testing.T) {
	lexJoin(t, `"a\"b\\c"`)               // escaped quote and backslash
	lexJoin(t, `"outer #{ {a: 1} } end"`) // nested braces inside interpolation
	lexJoin(t, "'\\''")                   // escaped single quote
	lexJoin(t, `/re\/gex/`)               // escaped slash in regexp
}

func TestPercentModuloNoSpace(t *testing.T) {
	// `n%2` — `%` directly followed by a digit is modulo, exercising the
	// "not actually a percent literal" fallback.
	for _, s := range []string{"n%2", "a%b", "x %= 2"} {
		lexJoin(t, s)
		for _, tk := range Lex(s) {
			if tk.Event == EvQwordsBeg || tk.Event == EvTstringBeg {
				t.Errorf("%q mislexed as percent literal", s)
			}
		}
	}
}

func TestWordListTrailingSeparatorEOF(t *testing.T) {
	// `%w[a ` ends on a separator → unterminated word list.
	if c, _ := CheckCode("%w[a "); c != More {
		t.Error("word list ending on separator")
	}
}

func TestHeredocBegRejections(t *testing.T) {
	// `<<` shapes that scanHeredocBeg rejects (falls back to shift operator).
	for _, s := range []string{
		"x <<~",     // <<~ then EOF
		"x <<\"END", // quoted delimiter never closed
		"x << 99",   // followed by a digit, not an identifier
		"x <<-",     // <<- then EOF
	} {
		lexJoin(t, s)
	}
}

func TestCharLiteralBegPositions(t *testing.T) {
	// `?` in value position followed by a two-letter word is the ternary form,
	// not a char literal (exercises charLiteralOK's lookahead reject).
	lexJoin(t, "[?ab]")
	// `?` at end of input in value position is not a char literal either.
	lexJoin(t, "x = ?")
	// genuine char literals in value position
	lexJoin(t, "x = ?z")
	lexJoin(t, "y = ?\\s")
}

func TestPercentBareEOF(t *testing.T) {
	// `%` in value position with nothing after it is the modulo operator token,
	// not a percent literal.
	lexJoin(t, "x = %")
	lexJoin(t, "x = % y")
}

func TestStringContentSpanningNewlineColor(t *testing.T) {
	// A double-quoted string whose content spans a newline colourises each
	// physical line with its own reset (subClear's with-newline branch).
	got := ColorizeCode("\"line1\nline2\"", true, true)
	if !strings.Contains(got, "\n") || !strings.Contains(got, "\x1b[0m") {
		t.Errorf("multiline string color: %q", got)
	}
}

func TestWordListEscapeAndNested(t *testing.T) {
	lexJoin(t, "%w[a\\ b c]")   // escaped space inside a word
	lexJoin(t, "%w{x {y} z}")   // nested braces
	lexJoin(t, "%i(a b\\)c d)") // escaped close-delim
}

func TestParseByLineTrailingTokenNoNewline(t *testing.T) {
	// A multi-line token (string content) whose last segment has no trailing
	// newline exercises the splitLinesKeep continue path in ParseByLine.
	res := ParseByLine(Lex("\"abc\ndef\""))
	if len(res) == 0 {
		t.Fatal("expected line results")
	}
}

func TestEmptyAndWhitespaceInput(t *testing.T) {
	// empty input is Complete (no tokens, no leading-dot error)
	if c, _ := CheckCode(""); c != Complete {
		t.Errorf("empty: %v", c)
	}
	if c, _ := CheckCode("   "); c != Complete {
		t.Errorf("spaces: %v", c)
	}
	if c, _ := CheckCode("\n"); c != Complete {
		t.Errorf("newline: %v", c)
	}
}

func TestDefKeywordMethodName(t *testing.T) {
	// `def if(x)` — a keyword used as a method name (the in_method_head keyword
	// receiver path that falls through to the arg state).
	for _, s := range []string{
		"def if(x); end",
		"def class; end",
		"def then(a); a; end",
	} {
		if c, _ := CheckCode(s); c == SyntaxError {
			t.Errorf("%q wrongly SyntaxError", s)
		}
	}
}

func TestRegexpFlagsFollowedByToken(t *testing.T) {
	// flags terminated by a non-flag character (a `]`) exercise isRegexpFlag's
	// false return.
	lexJoin(t, "[/re/i]")
	lexJoin(t, "x = /re/m + 1")
}

func TestIndentDifferenceRecursion(t *testing.T) {
	// White-box exercise of indentDifference's loop: a line whose innermost open
	// token is a (non-plain) heredoc recurses up to that heredoc's opening line.
	hd := Token{Event: EvHeredocBeg, Tok: "<<~A", Line: 1}
	lineResults := []LineResult{
		{PrevOpens: nil, MinDepth: 0},         // line 0 (the heredoc's opener line)
		{PrevOpens: []Token{hd}, MinDepth: 1}, // line 1 — inside the heredoc
	}
	lines := []string{"x = <<~A", "  body"}
	// from line 1, recurse to line 0 (opener), which has no opens → 0 - 0 = 0.
	if got := indentDifference(lines, lineResults, 1); got != 0 {
		t.Errorf("indentDifference recursion = %d, want 0", got)
	}

	// A plain (non-dashed) heredoc short-circuits to 0.
	plain := Token{Event: EvHeredocBeg, Tok: "<<A", Line: 1}
	lr2 := []LineResult{
		{PrevOpens: nil, MinDepth: 0},
		{PrevOpens: []Token{plain}, MinDepth: 1},
	}
	if got := indentDifference([]string{"x = <<A", "body"}, lr2, 1); got != 0 {
		t.Errorf("plain heredoc indentDifference = %d, want 0", got)
	}

	// A free-indent token (string beg) also recurses to its opening line.
	str := Token{Event: EvTstringBeg, Tok: `"`, Line: 1}
	lr3 := []LineResult{
		{PrevOpens: nil, MinDepth: 0},
		{PrevOpens: []Token{str}, MinDepth: 1},
	}
	if got := indentDifference([]string{`x = "a`, "  b"}, lr3, 1); got != 0 {
		t.Errorf("free-indent indentDifference = %d, want 0", got)
	}
}

func TestTakeTokensBounds(t *testing.T) {
	ts := []Token{{Tok: "a"}, {Tok: "b"}}
	if len(takeTokens(ts, 5)) != 2 {
		t.Error("takeTokens over-length should clamp to len")
	}
	if len(takeTokens(ts, -1)) != 0 {
		t.Error("takeTokens negative should clamp to 0")
	}
	if len(takeTokens(ts, 1)) != 1 {
		t.Error("takeTokens in-range")
	}
}

func TestScanWordListNestedAndEOF(t *testing.T) {
	lexJoin(t, "%w[a b c]")
	lexJoin(t, "%i{x y z}")
	// nested paired delimiter inside the list
	lexJoin(t, "%w(a (nested) b)")
	// unterminated list (EOF mid-word)
	if c, _ := CheckCode("%w[a b"); c != More {
		t.Error("unterminated word list")
	}
}
