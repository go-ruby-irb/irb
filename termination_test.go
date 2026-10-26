// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import (
	"testing"
	"time"
)

// Every input ends its analysis. A word list with a newline between two
// words, or after the last one, looped forever in scanWordList: the
// separator run stopped at the newline and the word run stopped before it,
// so the lexer never moved (found by go-notebook's `notebook irb`, where
// typing `%w[a b` and Enter hung the prompt).
func TestLexTerminates(t *testing.T) {
	for _, src := range []string{
		"%w[a b\n",
		"%w[a\nb]",
		"%W[\n]",
		"%i[\n  a\n\n  b ]",
		"%I[a\n",
		"x = %w(\n  a\n  b\n",
		"%w[\n",
		"%w[a b\n\n\n",
		"%w[a\r\nb]",
	} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			CheckCode(src)
			ColorizeCode(src, true, true)
			AutoIndent([]string{src, ""}, 1, true)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%q: the analysis did not end", src)
		}
	}
}

// FuzzCheckCode runs the analysis on arbitrary input: it must end, and not
// panic. (A loop shows as the fuzzer stalling on one input.)
func FuzzCheckCode(f *testing.F) {
	for _, s := range continuationCorpus {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		CheckCode(src)
	})
}
