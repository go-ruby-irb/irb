// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import "strings"

// This file is a port of IRB::NestingParser. Given a token stream it tracks the
// stack of still-open syntactic constructs (def/do/begin/if/{/[/(/heredoc/string
// …) so the REPL can decide how deeply nested the cursor is and whether the
// statement is complete. The algorithm mirrors MRI line-for-line.

// nestState is the secondary state attached to an open token while the nesting
// parser is mid-construct (e.g. parsing a method head or a for/while condition).
type nestState int

const (
	nsNone nestState = iota
	nsAliasUndef
	nsUnquotedSymbol
	nsLambdaHead
	nsMethodHead
	nsForWhileUntilCond
)

// openEntry is one frame on the open-construct stack: the token that opened it,
// its secondary parsing state, and integer args used by a couple of states.
type openEntry struct {
	tok   Token
	state nestState
	args  []string // method-head sub-states
	count int      // alias/undef remaining names
}

var nestingIgnore = map[Event]bool{
	EvSp: true, EvIgnoredNl: true, EvComment: true,
	EvEmbdocBeg: true, EvEmbdoc: true, EvEmbdocEnd: true,
}

func containsArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}

// ScanOpens walks tokens and, after each token, reports the current list of
// open tokens (the .tok of each frame) to the optional callback. It returns the
// final list of open tokens (plus still-pending heredocs).
func ScanOpens(tokens []Token, cb func(t Token, opens []Token)) []Token {
	var opens []openEntry
	var pendingHeredocs []Token
	firstTokenOnLine := true

	for _, t := range tokens {
		skip := false
		var last *openEntry
		if len(opens) > 0 {
			last = &opens[len(opens)-1]
		}
		if last != nil {
			switch last.state {
			case nsAliasUndef:
				skip = t.Event == EvKw
			case nsUnquotedSymbol:
				if !nestingIgnore[t.Event] {
					opens = opens[:len(opens)-1]
					skip = true
				}
			case nsLambdaHead:
				if t.Event == EvTlambeg || (t.Event == EvKw && t.Tok == "do") {
					opens = opens[:len(opens)-1]
				}
			case nsMethodHead:
				opens, skip = stepMethodHead(opens, t)
			case nsForWhileUntilCond:
				if t.Event == EvSemicolon || t.Event == EvNl || (t.Event == EvKw && t.Tok == "do") {
					if t.Event == EvKw && t.Tok == "do" {
						skip = true
					}
					opens[len(opens)-1].state = nsNone
				}
			}
		}

		if !skip {
			opens, pendingHeredocs = applyToken(opens, pendingHeredocs, t, firstTokenOnLine)
		}

		if t.Event == EvNl || t.Event == EvSemicolon {
			firstTokenOnLine = true
		} else if t.Event != EvSp {
			firstTokenOnLine = false
		}

		if len(pendingHeredocs) > 0 && strings.Contains(t.Tok, "\n") {
			for i := len(pendingHeredocs) - 1; i >= 0; i-- {
				opens = append(opens, openEntry{tok: pendingHeredocs[i]})
			}
			pendingHeredocs = nil
		}

		if len(opens) > 0 && opens[len(opens)-1].state == nsAliasUndef &&
			!nestingIgnore[t.Event] && t.Event != EvHeredocEnd {
			e := opens[len(opens)-1]
			opens = opens[:len(opens)-1]
			if e.count >= 1 {
				e.count--
				opens = append(opens, e)
			}
		}

		if cb != nil {
			cb(t, openTokens(opens))
		}
	}

	result := openTokens(opens)
	for i := len(pendingHeredocs) - 1; i >= 0; i-- {
		result = append(result, pendingHeredocs[i])
	}
	return result
}

func openTokens(opens []openEntry) []Token {
	out := make([]Token, len(opens))
	for i, e := range opens {
		out[i] = e.tok
	}
	return out
}

// OpenTokens returns the list of still-open tokens at the end of the stream.
func OpenTokens(tokens []Token) []Token {
	return ScanOpens(tokens, nil)
}

// LineResult holds, for one source line, the [line tokens] and the open-token
// lists before and after the line plus the minimum nesting depth reached within
// it — exactly the tuple NestingParser.parse_by_line yields.
type LineResult struct {
	Tokens    []Token
	PrevOpens []Token
	NextOpens []Token
	MinDepth  int
}

// ParseByLine is the port of NestingParser.parse_by_line: it splits the token
// stream into per-line nesting snapshots used by the auto-indent calculation.
func ParseByLine(tokens []Token) []LineResult {
	var output []LineResult
	var lineTokens []Token
	var prevOpens []Token
	minDepth := 0

	lastOpens := ScanOpens(tokens, func(t Token, opens []Token) {
		depth := len(opens)
		if len(opens) > 0 && tokenEq(t, opens[len(opens)-1]) {
			depth = len(opens) - 1
		}
		if depth < minDepth {
			minDepth = depth
		}
		if strings.Contains(t.Tok, "\n") {
			for _, line := range splitLinesKeep(t.Tok) {
				lineTokens = append(lineTokens, Token{Event: t.Event, Tok: line, State: t.State, Line: t.Line, Col: t.Col})
				if len(line) == 0 || line[len(line)-1] != '\n' {
					continue
				}
				nextOpens := append([]Token(nil), opens...)
				output = append(output, LineResult{Tokens: lineTokens, PrevOpens: prevOpens, NextOpens: nextOpens, MinDepth: minDepth})
				prevOpens = nextOpens
				minDepth = len(prevOpens)
				lineTokens = nil
			}
		} else {
			lineTokens = append(lineTokens, t)
		}
	})

	if len(lineTokens) > 0 {
		output = append(output, LineResult{Tokens: lineTokens, PrevOpens: prevOpens, NextOpens: lastOpens, MinDepth: minDepth})
	}
	return output
}

// tokenEq reports identity of two tokens for the depth check (same event, text
// and position).
func tokenEq(a, b Token) bool {
	return a.Event == b.Event && a.Tok == b.Tok && a.Line == b.Line && a.Col == b.Col
}

// splitLinesKeep splits s into lines, keeping the trailing newline on each line
// (like Ruby's String#each_line).
func splitLinesKeep(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func applyToken(opens []openEntry, pending []Token, t Token, firstTokenOnLine bool) ([]openEntry, []Token) {
	var last *openEntry
	if len(opens) > 0 {
		last = &opens[len(opens)-1]
	}
	switch t.Event {
	case EvKw:
		switch t.Tok {
		case "begin", "class", "module", "do", "case":
			opens = append(opens, openEntry{tok: t})
		case "end":
			if len(opens) > 0 {
				opens = opens[:len(opens)-1]
			}
		case "def":
			opens = append(opens, openEntry{tok: t, state: nsMethodHead, args: []string{"receiver", "name"}})
		case "if", "unless":
			if !t.AllBits(ExprLabel) {
				opens = append(opens, openEntry{tok: t})
			}
		case "while", "until":
			if !t.AllBits(ExprLabel) {
				opens = append(opens, openEntry{tok: t, state: nsForWhileUntilCond})
			}
		case "ensure", "rescue":
			if !t.AllBits(ExprLabel) {
				if len(opens) > 0 {
					opens = opens[:len(opens)-1]
				}
				opens = append(opens, openEntry{tok: t})
			}
		case "alias":
			opens = append(opens, openEntry{tok: t, state: nsAliasUndef, count: 2})
		case "undef":
			opens = append(opens, openEntry{tok: t, state: nsAliasUndef, count: 1})
		case "elsif", "else", "when":
			if len(opens) > 0 {
				opens = opens[:len(opens)-1]
			}
			opens = append(opens, openEntry{tok: t})
		case "for":
			opens = append(opens, openEntry{tok: t, state: nsForWhileUntilCond})
		case "in":
			if last != nil && last.tok.Event == EvKw &&
				(last.tok.Tok == "case" || last.tok.Tok == "in") && firstTokenOnLine {
				opens = opens[:len(opens)-1]
				opens = append(opens, openEntry{tok: t})
			}
		}
	case EvTlambda:
		opens = append(opens, openEntry{tok: t, state: nsLambdaHead})
	case EvLparen, EvLbracket, EvLbrace, EvTlambeg, EvEmbexprBeg, EvEmbdocBeg:
		opens = append(opens, openEntry{tok: t})
	case EvRparen, EvRbracket, EvRbrace, EvEmbexprEnd, EvEmbdocEnd:
		if len(opens) > 0 {
			opens = opens[:len(opens)-1]
		}
	case EvHeredocBeg:
		pending = append(pending, t)
	case EvHeredocEnd:
		if len(opens) > 0 {
			opens = opens[:len(opens)-1]
		}
	case EvBacktick:
		if t.State != ExprArg {
			opens = append(opens, openEntry{tok: t})
		}
	case EvTstringBeg, EvWordsBeg, EvQwordsBeg, EvSymbolsBeg, EvQsymbolsBeg, EvRegexpBeg:
		opens = append(opens, openEntry{tok: t})
	case EvTstringEnd, EvRegexpEnd, EvLabelEnd:
		if len(opens) > 0 {
			opens = opens[:len(opens)-1]
		}
	case EvSymbeg:
		if t.Tok == ":" {
			opens = append(opens, openEntry{tok: t, state: nsUnquotedSymbol})
		} else {
			opens = append(opens, openEntry{tok: t})
		}
	}
	return opens, pending
}

// stepMethodHead is the port of the :in_method_head branch of NestingParser; it
// advances the per-token state machine that recognises a method definition head
// (receiver, name, args, =) and decides when the head ends.
func stepMethodHead(opens []openEntry, t Token) ([]openEntry, bool) {
	if nestingIgnore[t.Event] {
		return opens, false
	}
	skip := false
	idx := len(opens) - 1
	args := opens[idx].args
	var nextArgs []string
	body := "" // "", "normal" or "oneliner"

	if containsArg(args, "receiver") {
		switch t.Event {
		case EvLparen, EvIvar, EvGvar, EvCvar:
			nextArgs = append(nextArgs, "dot")
		case EvKw:
			switch t.Tok {
			case "self", "true", "false", "nil":
				nextArgs = append(nextArgs, "arg", "dot")
			default:
				skip = true
				nextArgs = append(nextArgs, "arg")
			}
		case EvOp, EvBacktick:
			skip = true
			nextArgs = append(nextArgs, "arg")
		case EvIdent, EvConst:
			nextArgs = append(nextArgs, "arg", "dot")
		}
	}
	if containsArg(args, "dot") {
		if t.Event == EvPeriod || (t.Event == EvOp && t.Tok == "::") {
			nextArgs = append(nextArgs, "name")
		}
	}
	if containsArg(args, "name") {
		switch t.Event {
		case EvIdent, EvConst, EvOp, EvKw, EvBacktick:
			nextArgs = append(nextArgs, "arg")
			skip = true
		}
	}
	if containsArg(args, "arg") {
		switch t.Event {
		case EvNl, EvSemicolon:
			body = "normal"
		case EvLparen:
			nextArgs = append(nextArgs, "eq")
		default:
			if t.Event == EvOp && t.Tok == "=" {
				body = "oneliner"
			} else {
				nextArgs = append(nextArgs, "arg_without_paren")
			}
		}
	}
	if containsArg(args, "eq") {
		if t.Event == EvOp && t.Tok == "=" {
			body = "oneliner"
		} else {
			body = "normal"
		}
	}
	if containsArg(args, "arg_without_paren") {
		switch t.Event {
		case EvSemicolon, EvNl:
			body = "normal"
		default:
			nextArgs = append(nextArgs, "arg_without_paren")
		}
	}

	switch body {
	case "oneliner":
		opens = opens[:idx]
	case "normal":
		opens[idx].state = nsNone
		opens[idx].args = nil
	default:
		opens[idx].state = nsMethodHead
		opens[idx].args = nextArgs
	}
	return opens, skip
}
