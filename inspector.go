// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import "strings"

// This file ports the deterministic result-formatting parts of IRB::Inspector
// and IRB::Irb#output_value: turning an already-inspected value string into the
// `=> value` line, including the single-page truncation IRB applies when
// echo_on_assignment is :truncate. Producing the inspected string itself
// (calling #inspect / pp / YAML.dump on a live object) is the host's job — that
// requires the interpreter — so callers pass the inspected text in.

// FormatResult applies a return-format template (e.g. "=> %s\n") to an inspected
// value, exactly like Ruby's format(@context.return_format, content). A format
// with no "%" is returned verbatim (the :NULL / :CLASSIC style "%s\n" still has
// one; ":RETURN without %" prints the template itself, matching output_value).
func FormatResult(returnFormat, content string) string {
	if !strings.Contains(returnFormat, "%") {
		return returnFormat + "\n"
	}
	content = strings.TrimRight(content, "\n")
	return sprintfS(returnFormat, content)
}

// sprintfS replaces the single %s in format with s, leaving %% as a literal %.
// IRB's return formats only ever contain one %s, so a focused expander suffices
// and avoids pulling in fmt's full verb handling for this hot path.
func sprintfS(format, s string) string {
	var b strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) {
			switch format[i+1] {
			case 's':
				b.WriteString(s)
				i++
				continue
			case '%':
				b.WriteByte('%')
				i++
				continue
			}
		}
		b.WriteByte(format[i])
	}
	return b.String()
}

// TruncateResult ports the omit branch of IRB#output_value: keep only the first
// terminal "page" (one row of winWidth columns) of the inspected value and, when
// it overflows, append "..." (and a reset SGR when colourable). It returns the
// possibly-truncated content and whether truncation happened.
func TruncateResult(inspected string, winWidth int, newlineBeforeMultiline, colorable bool) (string, bool) {
	content, overflow := takeFirstPage(inspected, winWidth)
	if overflow {
		if newlineBeforeMultiline {
			content = "\n" + content
		}
		content += "..."
		if colorable {
			content += "\x1b[0m"
		}
	}
	return content, overflow
}

// takeFirstPage returns the first row (up to winWidth visible columns of the
// first line) and whether the value spilled past it. SGR escape sequences are
// passed through without counting toward the width, matching the way Reline's
// pager measures display width on a colourised string.
func takeFirstPage(s string, winWidth int) (string, bool) {
	if winWidth <= 0 {
		winWidth = 80
	}
	var b strings.Builder
	width := 0
	i := 0
	overflow := false
	for i < len(s) {
		c := s[i]
		if c == '\x1b' {
			// copy a CSI/SGR escape verbatim, not counting its width
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
					j++
				}
				if j < len(s) {
					j++ // final letter
				}
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		if c == '\n' {
			overflow = true
			break
		}
		if width >= winWidth {
			overflow = true
			break
		}
		b.WriteByte(c)
		width++
		i++
	}
	return b.String(), overflow
}

// InspectorMode names the built-in IRB inspectors (the keys of
// IRB::Inspector::INSPECTORS). Selecting and running them needs the interpreter
// (they call #inspect / pp / Marshal.dump / YAML.dump on a live object), so this
// type is just the deterministic name/alias registry a host uses to resolve a
// requested mode; the actual value rendering is a host seam.
type InspectorMode struct {
	Canonical string   // canonical name, e.g. "p"
	Aliases   []string // accepted aliases, e.g. "inspect"
}

// InspectorModes is the deterministic alias table mirroring INSPECTORS' keys.
var InspectorModes = []InspectorMode{
	{Canonical: "to_s", Aliases: []string{"false", "raw"}},
	{Canonical: "p", Aliases: []string{"inspect"}},
	{Canonical: "pp", Aliases: []string{"true", "pretty_inspect"}},
	{Canonical: "yaml", Aliases: []string{"YAML"}},
	{Canonical: "marshal", Aliases: []string{"Marshal", "MARSHAL"}},
}

// ResolveInspector maps a requested inspector name or alias to its canonical
// name, reporting whether it is known.
func ResolveInspector(name string) (string, bool) {
	for _, m := range InspectorModes {
		if m.Canonical == name {
			return m.Canonical, true
		}
		for _, a := range m.Aliases {
			if a == name {
				return m.Canonical, true
			}
		}
	}
	return "", false
}
