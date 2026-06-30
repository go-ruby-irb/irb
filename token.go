// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import "strings"

// Event is a Ripper-style lexer event name (the `on_*` symbol Ripper emits for
// each token). IRB's RubyLex, NestingParser and Color all dispatch on these
// event names plus the lexer State, so this package reproduces them faithfully
// rather than inventing its own token taxonomy. Only the events IRB actually
// inspects are produced; everything else collapses to a small set of generic
// events (e.g. on_op) exactly as Ripper does.
type Event string

// The Ripper events this port emits. Names match Ripper's `on_*` symbols so the
// continuation, nesting and colorize logic stays a line-by-line port of MRI.
const (
	EvInt            Event = "on_int"
	EvFloat          Event = "on_float"
	EvRational       Event = "on_rational"
	EvImaginary      Event = "on_imaginary"
	EvIdent          Event = "on_ident"
	EvConst          Event = "on_const"
	EvKw             Event = "on_kw"
	EvIvar           Event = "on_ivar"
	EvGvar           Event = "on_gvar"
	EvCvar           Event = "on_cvar"
	EvBackref        Event = "on_backref"
	EvOp             Event = "on_op"
	EvSp             Event = "on_sp"
	EvNl             Event = "on_nl"
	EvIgnoredNl      Event = "on_ignored_nl"
	EvComment        Event = "on_comment"
	EvSemicolon      Event = "on_semicolon"
	EvComma          Event = "on_comma"
	EvPeriod         Event = "on_period"
	EvLabel          Event = "on_label"
	EvLabelEnd       Event = "on_label_end"
	EvLparen         Event = "on_lparen"
	EvRparen         Event = "on_rparen"
	EvLbracket       Event = "on_lbracket"
	EvRbracket       Event = "on_rbracket"
	EvLbrace         Event = "on_lbrace"
	EvRbrace         Event = "on_rbrace"
	EvTstringBeg     Event = "on_tstring_beg"
	EvTstringContent Event = "on_tstring_content"
	EvTstringEnd     Event = "on_tstring_end"
	EvRegexpBeg      Event = "on_regexp_beg"
	EvRegexpEnd      Event = "on_regexp_end"
	EvSymbeg         Event = "on_symbeg"
	EvSymbolsBeg     Event = "on_symbols_beg"
	EvQsymbolsBeg    Event = "on_qsymbols_beg"
	EvWordsBeg       Event = "on_words_beg"
	EvQwordsBeg      Event = "on_qwords_beg"
	EvWordsSep       Event = "on_words_sep"
	EvBacktick       Event = "on_backtick"
	EvHeredocBeg     Event = "on_heredoc_beg"
	EvHeredocEnd     Event = "on_heredoc_end"
	EvEmbexprBeg     Event = "on_embexpr_beg"
	EvEmbexprEnd     Event = "on_embexpr_end"
	EvEmbvar         Event = "on_embvar"
	EvTlambda        Event = "on_tlambda"
	EvTlambeg        Event = "on_tlambeg"
	EvCHAR           Event = "on_CHAR"
	EvEmbdocBeg      Event = "on_embdoc_beg"
	EvEmbdoc         Event = "on_embdoc"
	EvEmbdocEnd      Event = "on_embdoc_end"
	EvEnd            Event = "on___end__"
	EvParseError     Event = "on_parse_error"
)

// Ripper lexer State bits (Ripper::EXPR_*). The continuation logic keys on these.
const (
	ExprBeg     = 0x1
	ExprEnd     = 0x2
	ExprEndArg  = 0x4
	ExprEndFn   = 0x8
	ExprArg     = 0x10
	ExprCmdArg  = 0x20
	ExprMid     = 0x40
	ExprFname   = 0x80
	ExprDot     = 0x100
	ExprClass   = 0x200
	ExprLabel   = 0x400
	ExprLabeled = 0x800
	ExprFitem   = 0x1000
)

// Token is one lexical token: a Ripper event, the exact source text, the lexer
// State after recognising it, and its 1-based [line, byte-column] position.
type Token struct {
	Event Event
	Tok   string
	State int
	Line  int
	Col   int
}

// AnyBits reports whether the token's State has any of the given bits set.
func (t Token) AnyBits(mask int) bool { return t.State&mask != 0 }

// AllBits reports whether the token's State has all the given bits set.
func (t Token) AllBits(mask int) bool { return t.State&mask == mask }

// reservedWords is the set of Ruby keywords recognised by the lexer as on_kw.
var reservedWords = map[string]bool{
	"__ENCODING__": true, "__LINE__": true, "__FILE__": true,
	"BEGIN": true, "END": true,
	"alias": true, "and": true,
	"begin": true, "break": true,
	"case": true, "class": true,
	"def": true, "defined?": true, "do": true,
	"else": true, "elsif": true, "end": true, "ensure": true,
	"false": true, "for": true,
	"if": true, "in": true,
	"module": true,
	"next":   true, "nil": true, "not": true,
	"or":   true,
	"redo": true, "rescue": true, "retry": true, "return": true,
	"self": true, "super": true,
	"then": true, "true": true,
	"undef": true, "unless": true, "until": true,
	"when": true, "while": true,
	"yield": true,
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

func isIdentCont(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\f' || b == '\v' }

// hasTrailingBackslashNewline reports whether the code ends with a backslash
// immediately followed by a newline (the explicit line-continuation IRB honours
// directly, before even tokenizing).
func hasTrailingBackslashNewline(code string) bool {
	return strings.HasSuffix(code, "\\\n")
}
