// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// The golden tests pin this library's continuation / indent / ltype / colorize
// output for the shared corpora. The golden files were generated from MRI's irb
// and re-verified by the oracle tests (TestOracleContinuation /
// TestOracleColorize), so they are an interpreter-independent record of IRB's
// behaviour. They keep the deterministic suite at 100% coverage on the lanes
// where ruby is absent (Windows, qemu cross-arch).
//
// Regenerate (only after the oracle is green) with:
//
//	IRB_GEN_GOLDEN=1 go test -run TestGenGolden
//
// then commit testdata/*.golden.

const (
	goldenContinuation = "testdata/continuation.golden"
	goldenColorize     = "testdata/colorize.golden"
	goldenIndent       = "testdata/indent.golden"
)

func continuationLine(s string) string {
	comp, opens := CheckCode(s)
	term := "complete"
	if comp == More {
		term = "more"
	}
	return term + "|" + strconv.Itoa(CalcIndentLevel(opens)) + "|" + LtypeFromOpenTokens(opens)
}

// TestGenGolden regenerates the golden files. It is a no-op unless
// IRB_GEN_GOLDEN is set, so it never runs in CI.
func TestGenGolden(t *testing.T) {
	if os.Getenv("IRB_GEN_GOLDEN") == "" {
		t.Skip("set IRB_GEN_GOLDEN=1 to regenerate golden files")
	}
	var cont []string
	for _, s := range continuationCorpus {
		cont = append(cont, continuationLine(s))
	}
	if err := os.WriteFile(goldenContinuation, []byte(strings.Join(cont, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var col []string
	for _, s := range colorCorpus {
		col = append(col, strconv.Quote(ColorizeCode(s, true, true)))
	}
	if err := os.WriteFile(goldenColorize, []byte(strings.Join(col, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var ind []string
	for _, c := range indentCorpus {
		lines := strings.Split(c.Code, "\n")
		ind = append(ind, strconv.Itoa(AutoIndent(lines, c.LineIndex, c.IsNewline)))
	}
	if err := os.WriteFile(goldenIndent, []byte(strings.Join(ind, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAutoIndentGolden(t *testing.T) {
	data, err := os.ReadFile(goldenIndent)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(want) != len(indentCorpus) {
		t.Fatalf("golden has %d lines, corpus has %d", len(want), len(indentCorpus))
	}
	for i, c := range indentCorpus {
		lines := strings.Split(c.Code, "\n")
		if got := strconv.Itoa(AutoIndent(lines, c.LineIndex, c.IsNewline)); got != want[i] {
			t.Errorf("auto-indent %q line=%d: got %s want %s", c.Code, c.LineIndex, got, want[i])
		}
	}
}

func TestCheckCodeGolden(t *testing.T) {
	data, err := os.ReadFile(goldenContinuation)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(want) != len(continuationCorpus) {
		t.Fatalf("golden has %d lines, corpus has %d", len(want), len(continuationCorpus))
	}
	for i, s := range continuationCorpus {
		if got := continuationLine(s); got != want[i] {
			t.Errorf("snippet %q:\n  got  = %q\n  want = %q", s, got, want[i])
		}
	}
}

func TestColorizeGolden(t *testing.T) {
	data, err := os.ReadFile(goldenColorize)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(want) != len(colorCorpus) {
		t.Fatalf("golden has %d lines, corpus has %d", len(want), len(colorCorpus))
	}
	for i, s := range colorCorpus {
		unq, err := strconv.Unquote(want[i])
		if err != nil {
			t.Fatalf("golden line %d not a quoted string: %v", i, err)
		}
		if got := ColorizeCode(s, true, true); got != unq {
			t.Errorf("colorize %q:\n  got  = %q\n  want = %q", s, got, unq)
		}
	}
}
