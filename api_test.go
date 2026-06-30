// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import (
	"strings"
	"testing"
)

// ---- token / lexer surface ----------------------------------------------

func TestTokenBits(t *testing.T) {
	tk := Token{State: ExprBeg | ExprDot}
	if !tk.AnyBits(ExprDot) || tk.AnyBits(ExprEnd) {
		t.Fatal("AnyBits")
	}
	if !tk.AllBits(ExprBeg|ExprDot) || tk.AllBits(ExprBeg|ExprEnd) {
		t.Fatal("AllBits")
	}
}

func TestHasTrailingBackslashNewline(t *testing.T) {
	if !hasTrailingBackslashNewline("a\\\n") {
		t.Fatal("want true")
	}
	if hasTrailingBackslashNewline("a\n") {
		t.Fatal("want false")
	}
}

func TestLexRoundTrips(t *testing.T) {
	// The joined token text must reconstruct the input exactly.
	inputs := []string{
		"def foo(a, b)\n  a + b\nend\n",
		"x = [1, 2, 3].map { |n| n * 2 }",
		"\"interp #{a + b} done\"",
		"s = <<~SQL\n  SELECT 1\nSQL\n",
		"# comment only\n",
		"=begin\ndoc\n=end\n",
		"%w[one two three]",
		"obj&.method&.chain",
		":symbol, :\"quoted\", :+",
		"$global @ivar @@cvar Const local",
		"0xFF 0b1010 0o17 1.5e3 2r 3i",
		"`backtick` /regexp/im %r{x}",
		"?a ?\\n",
		"a ... b",
		"\t  leading tabs and spaces",
	}
	for _, in := range inputs {
		var b strings.Builder
		for _, tk := range Lex(in) {
			b.WriteString(tk.Tok)
		}
		if b.String() != in {
			t.Errorf("round-trip mismatch:\n  in  = %q\n  out = %q", in, b.String())
		}
	}
}

func TestLexUnterminatedVariants(t *testing.T) {
	// Each should be reported as needing more input.
	for _, s := range []string{
		"\"open", "'open", "`open", "/open", "%w[open", "%i(open",
		"<<~H\n  body", ":", "=begin\nunfinished", "\"a#{1 +",
	} {
		if c, _ := CheckCode(s); c != More {
			t.Errorf("%q: want More, got %v", s, c)
		}
	}
}

func TestLexNumbersAndChars(t *testing.T) {
	toks := Lex("0xff 0b11 0o7 0d9 1_000 1.5 1e10 1.2e-3 2r 3i 4ri")
	var ints, floats, rats, imags int
	for _, tk := range toks {
		switch tk.Event {
		case EvInt:
			ints++
		case EvFloat:
			floats++
		case EvRational:
			rats++
		case EvImaginary:
			imags++
		}
	}
	if ints == 0 || floats == 0 || rats == 0 || imags == 0 {
		t.Fatalf("number events: int=%d float=%d rat=%d imag=%d", ints, floats, rats, imags)
	}
}

func TestLexGvarForms(t *testing.T) {
	toks := Lex("$foo $1 $& $` $' $+ $~ $! $0")
	var gvar, backref int
	for _, tk := range toks {
		switch tk.Event {
		case EvGvar:
			gvar++
		case EvBackref:
			backref++
		}
	}
	if gvar == 0 || backref == 0 {
		t.Fatalf("gvar=%d backref=%d", gvar, backref)
	}
}

func TestLexBacktickMethodName(t *testing.T) {
	// `def \`` defines the backtick method; the backtick is on_backtick, not a
	// command string.
	toks := Lex("def `(cmd); end")
	found := false
	for _, tk := range toks {
		if tk.Event == EvBacktick {
			found = true
		}
	}
	if !found {
		t.Fatal("expected on_backtick for method-name backtick")
	}
}

// ---- prompt --------------------------------------------------------------

func TestFormatPromptSpecs(t *testing.T) {
	ctx := PromptContext{IRBName: "irb", Main: "main", MainIns: "#<Object>"}
	cases := []struct {
		format       string
		ltype        string
		indent, line int
		want         string
	}{
		{"%N(%m):%03n> ", "", 0, 1, "irb(main):001> "},
		{"%N(%m):%03n:%i> ", "", 2, 12, "irb(main):012:2> "},
		{"%l ", `"`, 0, 1, `" `},
		{"%M", "", 0, 1, "#<Object>"},
		{"%i", "", 5, 1, "5"},
		{"%3i", "", 4, 1, "  4"},
		{"%i", "", -1, 1, "-"},
		{"%3i", "", -1, 1, "  -"},
		{"100%%done", "", 0, 1, "100%done"},
		{"%n", "", 0, 42, "42"},
		{"end%", "", 0, 1, "end%"},
		{"%z", "", 0, 1, ""},
	}
	for _, c := range cases {
		if got := FormatPrompt(c.format, ctx, c.ltype, c.indent, c.line); got != c.want {
			t.Errorf("FormatPrompt(%q) = %q, want %q", c.format, got, c.want)
		}
	}
}

func TestTruncatePromptMain(t *testing.T) {
	long := strings.Repeat("a", 40)
	got := truncatePromptMain(long)
	if len([]rune(got)) != 32 || !strings.HasSuffix(got, "...") {
		t.Errorf("truncate = %q (len %d)", got, len([]rune(got)))
	}
	// control characters become spaces
	if truncatePromptMain("a\x01b") != "a b" {
		t.Errorf("control char not replaced: %q", truncatePromptMain("a\x01b"))
	}
	if truncatePromptMain("short") != "short" {
		t.Error("short string changed")
	}
}

func TestGeneratePrompt(t *testing.T) {
	ctx := PromptContext{IRBName: "irb", Main: "main"}
	// primary
	if p := GeneratePrompt(PromptDefault, ctx, nil, false, 1, true, false); p != "irb(main):001> " {
		t.Errorf("primary = %q", p)
	}
	// continuation (open block)
	comp, opens := CheckCode("def foo")
	if comp != More {
		t.Fatal("setup")
	}
	if p := GeneratePrompt(PromptDefault, ctx, opens, true, 2, true, false); p != "irb(main):002* " {
		t.Errorf("continuation = %q", p)
	}
	// string continuation (ltype)
	_, sopens := CheckCode("\"abc")
	if p := GeneratePrompt(PromptDefault, ctx, sopens, true, 3, true, false); p != `irb(main):003" ` {
		t.Errorf("string-cont = %q", p)
	}
	// prompting off → empty
	if p := GeneratePrompt(PromptDefault, ctx, nil, false, 1, false, false); p != "" {
		t.Errorf("non-prompting = %q", p)
	}
	// auto-indent padding for INF_RUBY-style (no S/C prompt)
	_, bopens := CheckCode("if x")
	p := GeneratePrompt(PromptInfRuby, ctx, bopens, true, 2, true, true)
	if !strings.HasSuffix(p, "  ") {
		t.Errorf("auto-indent padding missing: %q", p)
	}
}

func TestPromptModesTable(t *testing.T) {
	for _, name := range []string{"DEFAULT", "SIMPLE", "CLASSIC", "INF_RUBY", "NULL"} {
		if _, ok := PromptModes[name]; !ok {
			t.Errorf("missing prompt mode %s", name)
		}
	}
	if PromptSimple.PromptI != ">> " {
		t.Error("SIMPLE prompt_i")
	}
}

// ---- color ---------------------------------------------------------------

func TestColorizeDisabled(t *testing.T) {
	if ColorizeCode("1 + 1", true, false) != "1 + 1" {
		t.Error("colorable=false should be identity")
	}
}

func TestColorizeHelper(t *testing.T) {
	if Colorize("x", []int{sgrRed}, false) != "x" {
		t.Error("Colorize colorable=false")
	}
	got := Colorize("x", []int{sgrRed, sgrBold}, true)
	if got != "\x1b[31m\x1b[1mx\x1b[0m" {
		t.Errorf("Colorize = %q", got)
	}
}

func TestColorMultiline(t *testing.T) {
	// each physical line gets its own clear before the newline
	got := ColorizeCode("# a\n# b", true, true)
	if strings.Count(got, "\x1b[0m") < 2 {
		t.Errorf("multiline colorize missing per-line clear: %q", got)
	}
}

func TestItoaInternal(t *testing.T) {
	if itoa(0) != "0" || itoa(31) != "31" || itoa(-5) != "-5" {
		t.Errorf("itoa broken: %q %q %q", itoa(0), itoa(31), itoa(-5))
	}
}

// ---- inspector -----------------------------------------------------------

func TestFormatResult(t *testing.T) {
	if FormatResult("=> %s\n", "42") != "=> 42\n" {
		t.Error("default return format")
	}
	if FormatResult("%s\n", "hi\n") != "hi\n" {
		t.Error("classic format trims newline")
	}
	// a format with no '%' is printed verbatim with a newline
	if FormatResult("nothing", "ignored") != "nothing\n" {
		t.Error("no-% format")
	}
	// %% is a literal percent
	if FormatResult("=> %s%%\n", "x") != "=> x%\n" {
		t.Errorf("literal percent: %q", FormatResult("=> %s%%\n", "x"))
	}
}

func TestTruncateResult(t *testing.T) {
	long := strings.Repeat("x", 100)
	got, overflow := TruncateResult(long, 20, false, false)
	if !overflow || !strings.HasSuffix(got, "...") {
		t.Errorf("truncate: %q overflow=%v", got, overflow)
	}
	// no overflow
	got, overflow = TruncateResult("short", 20, false, false)
	if overflow || got != "short" {
		t.Errorf("no-overflow: %q %v", got, overflow)
	}
	// newline triggers overflow + leading newline option
	got, overflow = TruncateResult("a\nb", 20, true, false)
	if !overflow || !strings.HasPrefix(got, "\n") {
		t.Errorf("multiline: %q", got)
	}
	// colourable appends reset
	got, _ = TruncateResult(long, 10, false, true)
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("colour reset missing: %q", got)
	}
	// SGR sequences are passed through without counting width
	colored := "\x1b[31m" + strings.Repeat("y", 5) + "\x1b[0m"
	got, overflow = TruncateResult(colored, 20, false, false)
	if overflow || !strings.Contains(got, "yyyyy") {
		t.Errorf("sgr passthrough: %q overflow=%v", got, overflow)
	}
	// winWidth<=0 defaults to 80
	if _, of := TruncateResult("tiny", 0, false, false); of {
		t.Error("default width should not overflow tiny")
	}
}

func TestResolveInspector(t *testing.T) {
	for in, want := range map[string]string{
		"p": "p", "inspect": "p", "pp": "pp", "true": "pp",
		"raw": "to_s", "YAML": "yaml", "Marshal": "marshal",
	} {
		if got, ok := ResolveInspector(in); !ok || got != want {
			t.Errorf("ResolveInspector(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
	if _, ok := ResolveInspector("nope"); ok {
		t.Error("unknown inspector should not resolve")
	}
}

// ---- history -------------------------------------------------------------

func TestHistoryModel(t *testing.T) {
	h := NewHistory()
	if h.Len() != 0 {
		t.Fatal("new history not empty")
	}
	h.Push("a")
	h.Push("b\nc")
	if h.Len() != 2 {
		t.Fatal("len")
	}
	if got := h.Entries(); got[1] != "b\nc" {
		t.Error("entries")
	}
	if v, ok := h.At(0); !ok || v != "a" {
		t.Error("At(0)")
	}
	if v, ok := h.At(-1); !ok || v != "b\nc" {
		t.Error("At(-1)")
	}
	if _, ok := h.At(99); ok {
		t.Error("At out of range")
	}
	if _, ok := h.At(-99); ok {
		t.Error("At under range")
	}
	h.Clear()
	if h.Len() != 0 {
		t.Error("clear")
	}
}

func TestSaveLimit(t *testing.T) {
	if SaveLimit(true) != DefaultEntryLimit {
		t.Error("true → default")
	}
	if SaveLimit(false) != 0 {
		t.Error("false → 0")
	}
	if SaveLimit(50) != 50 {
		t.Error("int passthrough")
	}
	if SaveLimit("weird") != 0 {
		t.Error("unknown → 0")
	}
}

func TestHistoryEncodeDecode(t *testing.T) {
	entries := []string{"single", "multi\nline\nentry"}
	enc := EncodeHistoryLines(entries)
	if enc[1] != "multi\\\nline\\\nentry" {
		t.Errorf("encode: %q", enc[1])
	}
	// decode merges backslash-continued lines (multiline mode)
	disk := strings.Split(strings.Join(enc, "\n"), "\n")
	dec := DecodeHistoryLines(disk, true)
	if len(dec) != 2 || dec[1] != "multi\nline\nentry" {
		t.Errorf("round-trip: %#v", dec)
	}
	// non-multiline keeps every line separate
	dec2 := DecodeHistoryLines([]string{"a\\", "b"}, false)
	if len(dec2) != 2 {
		t.Errorf("non-multiline: %#v", dec2)
	}
}

func TestTrimHistory(t *testing.T) {
	e := []string{"1", "2", "3", "4", "5"}
	if got := TrimHistory(e, 3); len(got) != 3 || got[0] != "3" {
		t.Errorf("trim: %#v", got)
	}
	if got := TrimHistory(e, 10); len(got) != 5 {
		t.Error("trim under limit")
	}
	if got := TrimHistory(e, -1); len(got) != 5 {
		t.Error("infinite limit")
	}
}

// ---- command -------------------------------------------------------------

func TestLookupCommand(t *testing.T) {
	if c, ok := LookupCommand("ls"); !ok || c != "irb_ls" {
		t.Errorf("alias ls = %q,%v", c, ok)
	}
	if c, ok := LookupCommand("exit"); !ok || c != "irb_exit" {
		t.Errorf("alias exit = %q", c)
	}
	if c, ok := LookupCommand("cd"); !ok || c != "cd" {
		t.Errorf("canonical cd = %q", c)
	}
	if _, ok := LookupCommand("definitely_not_a_command"); ok {
		t.Error("unknown command resolved")
	}
	if len(CommandNames()) != len(Commands) {
		t.Error("CommandNames count")
	}
}

func TestParseInput(t *testing.T) {
	// a known command
	p := ParseInput("ls -g foo", nil, false)
	if !p.IsCommand || p.Command != "irb_ls" || p.Arg != "-g foo" {
		t.Errorf("command parse: %+v", p)
	}
	// bare command, no arg
	if p := ParseInput("exit", nil, false); !p.IsCommand || p.Command != "irb_exit" {
		t.Errorf("bare command: %+v", p)
	}
	// a Ruby expression that happens to share a name with no command
	if p := ParseInput("foo + bar", nil, false); p.IsCommand {
		t.Error("expression misparsed as command")
	}
	// local variable shadows a command name
	if p := ParseInput("ls", map[string]bool{"ls": true}, false); p.IsCommand {
		t.Error("local var should shadow command")
	}
	// assignment is never a command
	if p := ParseInput("show_source = 1", nil, true); p.IsCommand {
		t.Error("assignment classified as command")
	}
	// arg starting with assignment op (but not == / =~) is an assignment
	if p := ParseInput("history += 1", nil, false); p.IsCommand {
		t.Error("op-assign arg should not be command")
	}
	// == and =~ are NOT assignment, so command dispatch still applies... but
	// `history == 1` is a comparison expression; command requires the name to be
	// a command and arg not assignment — here it is a command name with `== 1`.
	if p := ParseInput("history == 1", nil, false); !p.IsCommand {
		t.Error("history == 1 should still dispatch as command (== is not assign)")
	}
	// multiline input is never a command
	if p := ParseInput("ls\nmore", nil, false); p.IsCommand {
		t.Error("multiline command")
	}
	// empty input
	if p := ParseInput("   ", nil, false); p.IsCommand {
		t.Error("blank input")
	}
}

// ---- rubylex public surface ----------------------------------------------

func TestCheckCodeVerdicts(t *testing.T) {
	if c, _ := CheckCode("1 + 1"); c != Complete {
		t.Error("complete")
	}
	if c, _ := CheckCode("def foo"); c != More {
		t.Error("more")
	}
	if c, _ := CheckCode("end"); c != SyntaxError {
		t.Errorf("syntax error, got %v", c)
	}
	if c, _ := CheckCode("."); c != SyntaxError {
		t.Errorf("leading dot, got %v", c)
	}
	if Complete.String() != "complete" || More.String() != "more" || SyntaxError.String() != "syntax_error" {
		t.Error("String()")
	}
}

func TestLtypeVariants(t *testing.T) {
	cases := map[string]string{
		"\"abc": `"`,
		"'abc":  `'`,
		"/re":   "/",
		"`cmd":  "`",
		"<<~H":  `"`,
		"%q{a":  `'`,
		"%Q{a":  `"`,
	}
	for code, want := range cases {
		_, opens := CheckCode(code)
		if got := LtypeFromOpenTokens(opens); got != want {
			t.Errorf("ltype(%q) = %q want %q", code, got, want)
		}
	}
	if LtypeFromOpenTokens(nil) != "" {
		t.Error("empty opens → empty ltype")
	}
}

func TestParseByLineShape(t *testing.T) {
	res := ParseByLine(Lex("if a\n  b\nend"))
	if len(res) < 2 {
		t.Fatalf("expected multiple line results, got %d", len(res))
	}
	// second line is nested one deep
	if len(res[1].PrevOpens) != 1 {
		t.Errorf("line 2 prev opens = %d", len(res[1].PrevOpens))
	}
}
