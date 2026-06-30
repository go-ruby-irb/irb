// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import "strings"

// This file ports IRB::RubyLex#process_indent_level and its helpers: given the
// source lines typed so far, it computes the number of leading spaces IRB's
// auto-indent would put on the current line. It is a faithful port of the MRI
// algorithm, including the free-indent (string/regexp/symbol), heredoc and
// =begin/=end embdoc special cases.

var freeIndentTokens = map[Event]bool{
	EvTstringBeg: true, EvBacktick: true, EvRegexpBeg: true, EvSymbeg: true,
}

func freeIndentToken(t *Token) bool {
	return t != nil && freeIndentTokens[t.Event]
}

// AutoIndent computes the leading-space count IRB would apply to line lineIndex
// (0-based) of lines. isNewline indicates the cursor just moved to a fresh line
// (versus re-indenting the current one). It tokenizes the joined source and
// delegates to processIndentLevel.
func AutoIndent(lines []string, lineIndex int, isNewline bool) int {
	tokens := Lex(strings.Join(lines, "\n"))
	return processIndentLevel(tokens, lines, lineIndex, isNewline)
}

func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

// indentDifference ports RubyLex#indent_difference.
func indentDifference(lines []string, lineResults []LineResult, lineIndex int) int {
	for {
		res := lineResults[lineIndex]
		var openTok *Token
		if n := len(res.PrevOpens); n > 0 {
			openTok = &res.PrevOpens[n-1]
		}
		if openTok == nil || (openTok.Event != EvHeredocBeg && !freeIndentToken(openTok)) {
			indentLevel := CalcIndentLevel(takeTokens(res.PrevOpens, res.MinDepth))
			calculated := 2 * indentLevel
			actual := leadingSpaces(lines[lineIndex])
			return actual - calculated
		}
		if openTok.Event == EvHeredocBeg && heredocPlain.MatchString(openTok.Tok) {
			return 0
		}
		lineIndex = openTok.Line - 1
	}
}

func takeTokens(ts []Token, n int) []Token {
	if n > len(ts) {
		n = len(ts)
	}
	if n < 0 {
		n = 0
	}
	return ts[:n]
}

// processIndentLevel ports RubyLex#process_indent_level.
func processIndentLevel(tokens []Token, lines []string, lineIndex int, isNewline bool) int {
	lineResults := ParseByLine(tokens)
	var prevOpens, nextOpens []Token
	var minDepth int
	if lineIndex < len(lineResults) {
		r := lineResults[lineIndex]
		prevOpens, nextOpens, minDepth = r.PrevOpens, r.NextOpens, r.MinDepth
	} else if len(lineResults) > 0 {
		// last line empty
		last := lineResults[len(lineResults)-1]
		prevOpens = last.NextOpens
		nextOpens = last.NextOpens
		minDepth = len(nextOpens)
	}

	indent := 2 * CalcIndentLevel(takeTokens(prevOpens, minDepth))

	preserveIdx := lineIndex
	if isNewline {
		preserveIdx = lineIndex - 1
	}
	preserveIndent := 0
	if preserveIdx >= 0 && preserveIdx < len(lines) {
		preserveIndent = leadingSpaces(lines[preserveIdx])
	}

	var prevOpenToken, nextOpenToken *Token
	if n := len(prevOpens); n > 0 {
		prevOpenToken = &prevOpens[n-1]
	}
	if n := len(nextOpens); n > 0 {
		nextOpenToken = &nextOpens[n-1]
	}

	baseIndent := 0
	if prevOpenToken != nil {
		baseIndent = indentDifference(lines, lineResults, prevOpenToken.Line-1)
		if baseIndent < 0 {
			baseIndent = 0
		}
	}

	switch {
	case freeIndentToken(prevOpenToken):
		if isNewline && prevOpenToken.Line == lineIndex {
			return baseIndent + indent
		}
		return preserveIndent
	case eventOf(prevOpenToken) == EvEmbdocBeg || eventOf(nextOpenToken) == EvEmbdocBeg:
		if eventOf(prevOpenToken) == eventOf(nextOpenToken) {
			return preserveIndent
		}
		return 0
	case eventOf(prevOpenToken) == EvHeredocBeg:
		return heredocIndent(prevOpenToken, lines, lineResults, lineIndex, isNewline, prevOpens, nextOpens, nextOpenToken, baseIndent, indent, preserveIndent)
	default:
		return baseIndent + indent
	}
}

func eventOf(t *Token) Event {
	if t == nil {
		return ""
	}
	return t.Event
}

// heredocIndent ports the heredoc branch of process_indent_level.
func heredocIndent(prev *Token, lines []string, lineResults []LineResult, lineIndex int, isNewline bool, prevOpens, nextOpens []Token, nextOpenToken *Token, baseIndent, indent, preserveIndent int) int {
	tok := prev.Tok
	if len(prevOpens) <= len(nextOpens) {
		firstLineInHeredoc := isNewline && lineIndex < len(lines) && lines[lineIndex] == "" &&
			lineIndex-1 >= 0 && lineIndex-1 < len(lineResults) &&
			!sameLastOpen(lineResults[lineIndex-1].PrevOpens, nextOpenToken)
		switch {
		case firstLineInHeredoc:
			if heredocDashTilde.MatchString(tok) {
				return baseIndent + indent
			}
			return indent
		case strings.HasPrefix(tok, "<<~"):
			if baseIndent+indent > preserveIndent {
				return baseIndent + indent
			}
			return preserveIndent
		default:
			return preserveIndent
		}
	}
	// heredoc close
	prevLineIndentLevel := CalcIndentLevel(prevOpens)
	if heredocDashTilde.MatchString(tok) {
		return baseIndent + 2*(prevLineIndentLevel-1)
	}
	return 0
}

func sameLastOpen(opens []Token, t *Token) bool {
	if t == nil || len(opens) == 0 {
		return false
	}
	return tokenEq(opens[len(opens)-1], *t)
}
