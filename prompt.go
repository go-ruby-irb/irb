// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import (
	"fmt"
	"strconv"
	"strings"
)

// This file ports IRB's prompt machinery: the named PROMPT_MODE table and the
// %-spec expansion in IRB#format_prompt, plus the prompt-string selection in
// IRB#generate_prompt. These are entirely deterministic given the (mode, open
// tokens, line number, main object string) inputs.

// PromptMode is one of IRB's built-in :PROMPT_MODE values.
type PromptMode struct {
	Name       string
	PromptI    string // primary prompt
	PromptS    string // string-continuation prompt
	PromptC    string // statement-continuation prompt
	Return     string // return-value format
	AutoIndent bool
}

// Built-in prompt modes, copied from IRB.conf[:PROMPT]. The empty PromptI/S/C of
// :NULL / :INF_RUBY are represented as "".
var (
	PromptDefault = PromptMode{Name: "DEFAULT", PromptI: "%N(%m):%03n> ", PromptS: "%N(%m):%03n%l ", PromptC: "%N(%m):%03n* ", Return: "=> %s\n"}
	PromptSimple  = PromptMode{Name: "SIMPLE", PromptI: ">> ", PromptS: "%l> ", PromptC: "?> ", Return: "=> %s\n"}
	PromptClassic = PromptMode{Name: "CLASSIC", PromptI: "%N(%m):%03n:%i> ", PromptS: "%N(%m):%03n:%i%l ", PromptC: "%N(%m):%03n:%i* ", Return: "%s\n"}
	PromptInfRuby = PromptMode{Name: "INF_RUBY", PromptI: "%N(%m):%03n> ", PromptS: "", PromptC: "", Return: "%s\n", AutoIndent: true}
	PromptNull    = PromptMode{Name: "NULL", PromptI: "", PromptS: "", PromptC: "", Return: "%s\n"}
)

// PromptModes maps the symbol name to the built-in mode.
var PromptModes = map[string]PromptMode{
	"DEFAULT":  PromptDefault,
	"SIMPLE":   PromptSimple,
	"CLASSIC":  PromptClassic,
	"INF_RUBY": PromptInfRuby,
	"NULL":     PromptNull,
}

const (
	promptMainTruncateLength   = 32
	promptMainTruncateOmission = "..."
)

// PromptContext carries the per-session values the %-specs interpolate.
type PromptContext struct {
	IRBName string // %N — the irb program name (default "irb")
	Main    string // %m — main object's to_s
	MainIns string // %M — main object's inspect
}

// FormatPrompt expands one prompt format string the way IRB#format_prompt does.
// ltype is the literal-type character (from LtypeFromOpenTokens), indent is the
// nesting level, lineNo is the 1-based line number.
func FormatPrompt(format string, ctx PromptContext, ltype string, indent, lineNo int) string {
	var b strings.Builder
	runes := []byte(format)
	i := 0
	for i < len(runes) {
		c := runes[i]
		if c != '%' {
			b.WriteByte(c)
			i++
			continue
		}
		// %[width]X
		j := i + 1
		width := ""
		for j < len(runes) && runes[j] >= '0' && runes[j] <= '9' {
			width += string(runes[j])
			j++
		}
		if j >= len(runes) {
			// trailing '%' with no spec — Ripper's gsub leaves it unmatched.
			b.WriteByte('%')
			i++
			continue
		}
		spec := runes[j]
		i = j + 1
		b.WriteString(expandSpec(spec, width, ctx, ltype, indent, lineNo))
	}
	return b.String()
}

func expandSpec(spec byte, width string, ctx PromptContext, ltype string, indent, lineNo int) string {
	switch spec {
	case 'N':
		return ctx.IRBName
	case 'm':
		return truncatePromptMain(ctx.Main)
	case 'M':
		return truncatePromptMain(ctx.MainIns)
	case 'l':
		return ltype
	case 'i':
		if indent < 0 {
			if width != "" {
				return rjust("-", atoi(width))
			}
			return "-"
		}
		if width != "" {
			return fmt.Sprintf("%"+width+"d", indent)
		}
		return strconv.Itoa(indent)
	case 'n':
		if width != "" {
			return fmt.Sprintf("%"+width+"d", lineNo)
		}
		return strconv.Itoa(lineNo)
	case '%':
		if width == "" {
			return "%"
		}
		return ""
	default:
		return ""
	}
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func rjust(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(s)) + s
}

// truncatePromptMain ports IRB#truncate_prompt_main: cap the main-object string
// at 32 chars with a "..." ellipsis and replace control characters with spaces.
func truncatePromptMain(s string) string {
	rs := []rune(s)
	if len(rs) > promptMainTruncateLength {
		rs = append(rs[:promptMainTruncateLength-len([]rune(promptMainTruncateOmission))], []rune(promptMainTruncateOmission)...)
		s = string(rs)
	}
	var b strings.Builder
	for _, r := range s {
		if r <= 0x1F {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// GeneratePrompt ports IRB::Irb#generate_prompt: it picks the I/S/C format based
// on the open tokens and whether the statement continues, computes ltype/indent
// from the open tokens, and applies auto-indent padding when requested. continue
// is the caller's notion of statement continuation (e.g. from CheckCode==More);
// opens are the open construct tokens; lineNo is the current line number.
func GeneratePrompt(mode PromptMode, ctx PromptContext, opens []Token, cont bool, lineNo int, prompting, autoIndent bool) string {
	ltype := LtypeFromOpenTokens(opens)
	indent := CalcIndentLevel(opens)
	cont = len(opens) > 0 || cont

	var f string
	switch {
	case ltype != "":
		f = mode.PromptS
	case cont:
		f = mode.PromptC
	default:
		f = mode.PromptI
	}

	var p string
	if prompting {
		p = FormatPrompt(f, ctx, ltype, indent, lineNo)
	}
	if autoIndent {
		if ltype == "" {
			promptI := FormatPrompt(mode.PromptI, ctx, ltype, indent, lineNo)
			ind := len(lastLine(promptI)) + indent*2 - len(p)
			if ind > 0 {
				p += strings.Repeat(" ", ind)
			}
		}
	}
	return p
}

func lastLine(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}
