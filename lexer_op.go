// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

// multiCharOps lists the multi-character operators, longest first, so the
// scanner matches greedily exactly as Ruby's lexer does.
var multiCharOps = []string{
	"<=>", "===", "**=", "...", "&&=", "||=", "<<=", ">>=",
	"==", "!=", ">=", "<=", "&&", "||", "**", "<<", ">>",
	"+=", "-=", "*=", "/=", "%=", "|=", "&=", "^=", "=~", "!~",
	"->", "=>", "..", "::", "&.",
}

// opEndsValue maps an operator to whether the lexer state after it is a "value
// already produced" position. Most operators reset to EXPR_BEG (a value is
// expected next); `]`, `)` etc. are handled in scanPunct.
func (l *lexer) scanOp() {
	line, col := l.startPos()
	rest := l.src[l.pos:]
	for _, op := range multiCharOps {
		if hasOpPrefix(rest, op) {
			l.advance(len(op))
			if op == "->" {
				l.state = ExprEndFn
				l.lambdaHeads++
				l.toks = append(l.toks, Token{Event: EvTlambda, Tok: op, State: l.state, Line: line, Col: col})
				return
			}
			// Every operator leaves the lexer expecting a value next (EXPR_BEG).
			// The scope `::` and method `.` tokens are emitted by scanColon and
			// scanPunct, not here, so no per-operator state table is needed.
			l.state = ExprBeg
			l.toks = append(l.toks, Token{Event: EvOp, Tok: op, State: l.state, Line: line, Col: col})
			return
		}
	}
	// single-char operator
	c := l.peek()
	l.advance(1)
	l.state = ExprBeg
	l.toks = append(l.toks, Token{Event: EvOp, Tok: string(c), State: l.state, Line: line, Col: col})
}

func hasOpPrefix(s, op string) bool {
	if len(s) < len(op) {
		return false
	}
	return s[:len(op)] == op
}

// scanPunct handles the structural punctuation: parens/brackets/braces, comma,
// semicolon, period, and falls back to scanOp for anything operator-shaped.
func (l *lexer) scanPunct(c byte) {
	line, col := l.startPos()
	switch c {
	case '(':
		l.advance(1)
		l.state = ExprBeg
		l.toks = append(l.toks, Token{Event: EvLparen, Tok: "(", State: l.state, Line: line, Col: col})
	case ')':
		l.advance(1)
		l.state = ExprEndFn
		l.toks = append(l.toks, Token{Event: EvRparen, Tok: ")", State: l.state, Line: line, Col: col})
	case '[':
		l.advance(1)
		l.state = ExprBeg
		l.toks = append(l.toks, Token{Event: EvLbracket, Tok: "[", State: l.state, Line: line, Col: col})
	case ']':
		l.advance(1)
		l.state = ExprEnd
		l.toks = append(l.toks, Token{Event: EvRbracket, Tok: "]", State: l.state, Line: line, Col: col})
	case '{':
		l.advance(1)
		ev := EvLbrace
		if l.lambdaHeads > 0 {
			// `->{ }` / `->(x){ }`: this brace opens the lambda body.
			l.lambdaHeads--
			ev = EvTlambeg
		}
		l.braceDepth++ // both on_lbrace and on_tlambeg are closed by `}`
		l.state = ExprBeg
		l.toks = append(l.toks, Token{Event: ev, Tok: "{", State: l.state, Line: line, Col: col})
	case '}':
		l.advance(1)
		// A `}` with no matching open brace is lexed by Ripper as on_embexpr_end
		// with state EXPR_BEG (it assumes a broken interpolation), which makes
		// IRB keep reading. With a matching brace it is a normal closer.
		if l.braceDepth > 0 {
			l.braceDepth--
			l.state = ExprEnd
			l.toks = append(l.toks, Token{Event: EvRbrace, Tok: "}", State: l.state, Line: line, Col: col})
		} else {
			l.state = ExprBeg
			l.toks = append(l.toks, Token{Event: EvEmbexprEnd, Tok: "}", State: l.state, Line: line, Col: col})
		}
	case ',':
		l.advance(1)
		l.state = ExprBeg
		l.toks = append(l.toks, Token{Event: EvComma, Tok: ",", State: l.state, Line: line, Col: col})
	case ';':
		l.advance(1)
		l.state = ExprBeg
		l.toks = append(l.toks, Token{Event: EvSemicolon, Tok: ";", State: l.state, Line: line, Col: col})
	case '.':
		// `.method` (period) vs `..`/`...` range (operator).
		if l.peekAt(1) == '.' {
			l.scanOp()
			return
		}
		l.advance(1)
		l.state = ExprDot
		l.toks = append(l.toks, Token{Event: EvPeriod, Tok: ".", State: l.state, Line: line, Col: col})
	default:
		l.scanOp()
	}
}
