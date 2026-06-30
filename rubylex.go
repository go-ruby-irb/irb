// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import (
	"regexp"
	"strings"
)

// This file ports the deterministic parts of IRB::RubyLex — the input-analysis
// logic that drives the multi-line continuation prompt. Given the source typed
// so far it decides whether the statement is COMPLETE, needs MORE input, or is a
// SYNTAX ERROR, and computes the nesting/indent level. The pieces that need a
// live interpreter in MRI (RubyVM::InstructionSequence.compile to classify the
// exact syntax-error variety) are replaced here by a self-contained
// classification driven by the token stream and open-construct stack, which
// reproduces IRB's decisions on the constructs a REPL meets.

// Completeness is the verdict for accumulated input.
type Completeness int

const (
	// Complete means the statement is finished and ready to evaluate.
	Complete Completeness = iota
	// More means IRB should keep reading (open block/literal/heredoc, trailing
	// operator or backslash continuation).
	More
	// SyntaxError means the input is unrecoverably wrong (e.g. a stray `end`).
	SyntaxError
)

func (c Completeness) String() string {
	switch c {
	case Complete:
		return "complete"
	case More:
		return "more"
	default:
		return "syntax_error"
	}
}

// rangeOp matches an endless-range operator (.. or ...).
var rangeOp = regexp.MustCompile(`^\.\.\.?$`)

// CheckCode analyses code and returns its completeness verdict together with the
// list of still-open construct tokens (useful for the prompt). It is the public
// entry point a REPL host calls after each line.
func CheckCode(code string) (Completeness, []Token) {
	res := lexInternal(code)
	opens := OpenTokens(res.tokens)
	return classify(code, res, opens), opens
}

// classify reproduces RubyLex#code_terminated? combined with #check_code_syntax.
func classify(code string, res lexResult, opens []Token) Completeness {
	switch checkSyntax(res, opens) {
	case syntaxUnrecoverable:
		return SyntaxError
	case syntaxRecoverable:
		return More
	default: // valid
		if shouldContinue(res.tokens) {
			return More
		}
		return Complete
	}
}

type syntaxResult int

const (
	syntaxValid syntaxResult = iota
	syntaxRecoverable
	syntaxUnrecoverable
)

// checkSyntax classifies the lexical state the way MRI's compile step does, but
// from tokens alone. Unterminated literals/heredocs are recoverable (need more);
// a stray `end` (more closers than openers) is unrecoverable; otherwise valid.
func checkSyntax(res lexResult, opens []Token) syntaxResult {
	if res.unterminated || res.openHeredocs > 0 {
		return syntaxRecoverable
	}
	if hasUnbalancedEnd(res.tokens) {
		return syntaxUnrecoverable
	}
	if leadingDotError(res.tokens) {
		return syntaxUnrecoverable
	}
	if len(opens) > 0 {
		// open block/paren with no syntax error of its own → recoverable.
		return syntaxRecoverable
	}
	return syntaxValid
}

// hasUnbalancedEnd reports whether the keyword/bracket nesting closes more times
// than it opens (e.g. a leading `end`), which is an unrecoverable syntax error.
func hasUnbalancedEnd(tokens []Token) bool {
	depth := 0
	min := 0
	for _, t := range tokens {
		switch {
		case t.Event == EvKw && isOpenKw(t):
			depth++
		case t.Event == EvKw && t.Tok == "end":
			depth--
		case t.Event == EvLparen || t.Event == EvLbracket || t.Event == EvLbrace:
			depth++
		case t.Event == EvRparen || t.Event == EvRbracket || t.Event == EvRbrace:
			depth--
		}
		if depth < min {
			min = depth
		}
	}
	return min < 0
}

func isOpenKw(t Token) bool {
	switch t.Tok {
	case "begin", "class", "module", "do", "case", "def":
		return true
	case "if", "unless", "while", "until":
		return !t.AllBits(ExprLabel)
	case "for":
		return true
	}
	return false
}

// leadingDotError reports a bare leading `.` with nothing before it.
func leadingDotError(tokens []Token) bool {
	for _, t := range tokens {
		if t.Event == EvSp || t.Event == EvNl || t.Event == EvIgnoredNl {
			continue
		}
		return t.Event == EvPeriod
	}
	return false
}

// shouldContinue is the port of RubyLex#should_continue?: examine the last
// significant token to see whether IRB must read another line (trailing
// operator, `.`, backslash, EXPR_BEG/EXPR_DOT state).
func shouldContinue(tokens []Token) bool {
	if n := len(tokens); n > 0 {
		last := tokens[n-1]
		if last.Event == EvSp && last.Tok == "\\\n" {
			return true
		}
	}
	for i := len(tokens) - 1; i >= 0; i-- {
		t := tokens[i]
		switch t.Event {
		case EvSp, EvNl, EvIgnoredNl, EvComment, EvEmbdocBeg, EvEmbdoc, EvEmbdocEnd:
			continue
		case EvRegexpEnd, EvHeredocEnd, EvSemicolon:
			return false
		default:
			if t.Event == EvOp && rangeOp.MatchString(t.Tok) {
				return false
			}
			return t.AnyBits(ExprBeg | ExprDot)
		}
	}
	return false
}

// CalcIndentLevel ports RubyLex#calc_indent_level: the nesting depth implied by
// a list of open tokens, used for the %i prompt spec and auto-indent.
func CalcIndentLevel(opens []Token) int {
	level := 0
	for i, t := range opens {
		switch t.Event {
		case EvHeredocBeg:
			if i+1 >= len(opens) || opens[i+1].Event != EvHeredocBeg {
				if heredocDashTilde.MatchString(t.Tok) {
					level++
				} else {
					level = 0
				}
			}
		case EvTstringBeg, EvRegexpBeg, EvSymbeg, EvBacktick:
			if strings.HasPrefix(t.Tok, "%") {
				level++
			}
		case EvEmbdocBeg:
			level = 0
		default:
			if t.Tok != "alias" && t.Tok != "undef" {
				level++
			}
		}
	}
	return level
}

var (
	heredocDashTilde = regexp.MustCompile(`^<<[~-]`)
	heredocPlain     = regexp.MustCompile(`^<<[^-~]`)
	heredocQuoted    = regexp.MustCompile("<<[-~]?(['\"`])(\\w+)(['\"`])")
)

// ltypeTokens are the open-literal events that contribute an ltype character.
// Word-list literals (%w/%i/%W/%I) are intentionally absent: this tokenizer
// always closes them (Ripper retags their final word as on_tstring_end), so a
// word-list beg never remains in the open-token list.
var ltypeTokens = map[Event]bool{
	EvHeredocBeg: true, EvTstringBeg: true, EvRegexpBeg: true,
	EvSymbeg: true, EvBacktick: true,
}

// LtypeFromOpenTokens ports RubyLex#ltype_from_open_tokens: the literal-type
// character ("/", `"`, etc.) used by the %l prompt spec while inside an open
// string / regexp / word-list / heredoc.
func LtypeFromOpenTokens(opens []Token) string {
	var start *Token
	for i := len(opens) - 1; i >= 0; i-- {
		if ltypeTokens[opens[i].Event] {
			start = &opens[i]
			break
		}
	}
	if start == nil {
		return ""
	}
	switch start.Event {
	case EvTstringBeg:
		switch {
		case start.Tok == `'`:
			return `'`
		case matchRe(`^%q.$`, start.Tok):
			return `'`
		default:
			// `"`, `%(`, `%Q(` and any other double-quoted form.
			return `"`
		}
	case EvRegexpBeg:
		return "/"
	case EvSymbeg:
		return ":"
	case EvBacktick:
		return "`"
	default: // EvHeredocBeg — the only remaining ltype token
		if m := heredocQuoted.FindStringSubmatch(start.Tok); m != nil {
			return m[1]
		}
		return `"`
	}
}

func matchRe(pat, s string) bool {
	return regexp.MustCompile(pat).MatchString(s)
}
