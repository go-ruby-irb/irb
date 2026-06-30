// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import "strings"

// This file ports IRB::Color: the deterministic mapping from a Ripper token
// stream to ANSI SGR escape sequences. colorize_code re-lexes the source and
// wraps each token in the colour its (event, lexer-state) pair selects, exactly
// as MRI does. The tty / $stdout.tty? gating is a host concern, so callers pass
// `colorable` explicitly.

// SGR attribute codes (IRB::Color constants).
const (
	sgrClear     = 0
	sgrBold      = 1
	sgrUnderline = 4
	sgrReverse   = 7
	sgrBlack     = 30
	sgrRed       = 31
	sgrGreen     = 32
	sgrYellow    = 33
	sgrBlue      = 34
	sgrMagenta   = 35
	sgrCyan      = 36
	sgrWhite     = 37
)

const exprAll = -1

// colorSpec is a colour sequence plus the lexer-state mask it applies under.
type colorSpec struct {
	seq   []int
	exprs int
}

// tokenSeqExprs is the port of IRB::Color::TOKEN_SEQ_EXPRS.
var tokenSeqExprs = map[Event]colorSpec{
	EvCHAR:           {[]int{sgrBlue, sgrBold}, exprAll},
	EvBacktick:       {[]int{sgrRed, sgrBold}, exprAll},
	EvComment:        {[]int{sgrBlue, sgrBold}, exprAll},
	EvConst:          {[]int{sgrBlue, sgrBold, sgrUnderline}, exprAll},
	EvEmbexprBeg:     {[]int{sgrRed}, exprAll},
	EvEmbexprEnd:     {[]int{sgrRed}, exprAll},
	EvEmbvar:         {[]int{sgrRed}, exprAll},
	EvFloat:          {[]int{sgrMagenta, sgrBold}, exprAll},
	EvGvar:           {[]int{sgrGreen, sgrBold}, exprAll},
	EvBackref:        {[]int{sgrGreen, sgrBold}, exprAll},
	EvHeredocBeg:     {[]int{sgrRed}, exprAll},
	EvHeredocEnd:     {[]int{sgrRed}, exprAll},
	EvIdent:          {[]int{sgrBlue, sgrBold}, ExprEndFn},
	EvImaginary:      {[]int{sgrBlue, sgrBold}, exprAll},
	EvInt:            {[]int{sgrBlue, sgrBold}, exprAll},
	EvKw:             {[]int{sgrGreen}, exprAll},
	EvLabel:          {[]int{sgrMagenta}, exprAll},
	EvLabelEnd:       {[]int{sgrRed, sgrBold}, exprAll},
	EvQsymbolsBeg:    {[]int{sgrRed, sgrBold}, exprAll},
	EvQwordsBeg:      {[]int{sgrRed, sgrBold}, exprAll},
	EvRational:       {[]int{sgrBlue, sgrBold}, exprAll},
	EvRegexpBeg:      {[]int{sgrRed, sgrBold}, exprAll},
	EvRegexpEnd:      {[]int{sgrRed, sgrBold}, exprAll},
	EvSymbeg:         {[]int{sgrYellow}, exprAll},
	EvSymbolsBeg:     {[]int{sgrRed, sgrBold}, exprAll},
	EvTstringBeg:     {[]int{sgrRed, sgrBold}, exprAll},
	EvTstringContent: {[]int{sgrRed}, exprAll},
	EvTstringEnd:     {[]int{sgrRed, sgrBold}, exprAll},
	EvWordsBeg:       {[]int{sgrRed, sgrBold}, exprAll},
	EvParseError:     {[]int{sgrRed, sgrReverse}, exprAll},
	EvEnd:            {[]int{sgrGreen}, exprAll},
}

// tokenKeywords are the literal strings re-coloured cyan/bold when seen as
// on_kw / on_const (the pseudo-keyword constants).
var tokenKeywords = map[Event]map[string]bool{
	EvKw:    {"nil": true, "self": true, "true": true, "false": true, "__FILE__": true, "__LINE__": true, "__ENCODING__": true},
	EvConst: {"ENV": true},
}

// Colorize wraps text in the given SGR codes (IRB::Color.colorize). When
// colorable is false it returns text unchanged.
func Colorize(text string, seq []int, colorable bool) string {
	if !colorable {
		return text
	}
	var b strings.Builder
	for _, s := range seq {
		b.WriteString(sgr(s))
	}
	b.WriteString(text)
	b.WriteString(clearSeq(colorable))
	return b.String()
}

func sgr(code int) string {
	return "\x1b[" + itoa(code) + "m"
}

func clearSeq(colorable bool) string {
	if !colorable {
		return ""
	}
	return sgr(sgrClear)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ColorizeCode is the port of IRB::Color.colorize_code: it re-lexes code and
// emits it with each token wrapped in its colour. When colorable is false it
// returns code unchanged. complete=false suppresses colouring an only-incomplete
// trailing error (matching IRB's `allow_last_error`).
func ColorizeCode(code string, complete, colorable bool) string {
	if !colorable {
		return code
	}
	symState := newSymbolState()
	var colored strings.Builder
	tokens := Lex(code)
	pos := 0
	for _, t := range tokens {
		// emit any gap (uncolourable bytes) between tokens verbatim
		// (our lexer is gap-free, so this is a no-op kept for fidelity).
		inSymbol := symState.scanToken(t.Event)
		for _, line := range splitLinesKeep(t.Tok) {
			if seq := dispatchSeq(t.Event, t.State, t.Tok, inSymbol); seq != nil {
				for _, s := range seq {
					colored.WriteString(sgr(s))
				}
				colored.WriteString(subClear(line, colorable))
			} else {
				colored.WriteString(line)
			}
		}
		pos += len(t.Tok)
	}
	return colored.String()
}

// subClear appends the clear sequence at the end of a line (before any trailing
// newline), reproducing Ruby's `line.sub(/\Z/, clear)`.
func subClear(line string, colorable bool) string {
	c := clearSeq(colorable)
	if strings.HasSuffix(line, "\n") {
		return line[:len(line)-1] + c + "\n"
	}
	return line + c
}

// dispatchSeq is the port of IRB::Color.dispatch_seq.
func dispatchSeq(event Event, state int, str string, inSymbol bool) []int {
	if inSymbol {
		return []int{sgrYellow}
	}
	if kws, ok := tokenKeywords[event]; ok && kws[str] {
		return []int{sgrCyan, sgrBold}
	}
	if spec, ok := tokenSeqExprs[event]; ok {
		if state&spec.exprs != 0 {
			return spec.seq
		}
	}
	return nil
}

// symbolState ports IRB::Color::SymbolState: it tracks whether the current token
// is part of a Symbol literal (so it is coloured yellow).
type symbolState struct {
	stack []bool
}

func newSymbolState() *symbolState { return &symbolState{} }

func (s *symbolState) top() (bool, bool) {
	if len(s.stack) == 0 {
		return false, false
	}
	return s.stack[len(s.stack)-1], true
}

func (s *symbolState) push(v bool) { s.stack = append(s.stack, v) }

func (s *symbolState) pop() {
	if len(s.stack) > 0 {
		s.stack = s.stack[:len(s.stack)-1]
	}
}

// scanToken returns whether the given token is part of a Symbol.
func (s *symbolState) scanToken(event Event) bool {
	prev, _ := s.top()
	switch event {
	case EvSymbeg, EvSymbolsBeg, EvQsymbolsBeg:
		s.push(true)
	case EvIdent, EvOp, EvConst, EvIvar, EvCvar, EvGvar, EvKw, EvBacktick:
		if last, ok := s.top(); ok && last {
			s.pop()
			return prev
		}
	case EvTstringBeg:
		s.push(false)
	case EvEmbexprBeg:
		s.push(false)
		return prev
	case EvTstringEnd:
		s.pop()
		return prev
	case EvEmbexprEnd:
		s.pop()
	}
	cur, _ := s.top()
	return cur
}
