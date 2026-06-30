// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// The oracle tests run the real `irb` gem (MRI) over the shared corpora and
// assert this library makes the same continuation, indent, ltype and colorize
// decisions. They skip themselves when ruby is unavailable (the Windows lane and
// the cross-arch qemu lanes), where the committed golden files keep the
// deterministic suite at 100% coverage instead.

// rubyBin locates a usable `ruby` once, skipping the test when absent.
func rubyBin(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("ruby oracle skipped on Windows")
	}
	path, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("ruby not on PATH; skipping MRI oracle")
	}
	// Require a modern irb whose RubyLex/Color/NestingParser API this port
	// targets. Old bundled IRBs (e.g. macOS system ruby 2.6) load the files but
	// lack IRB::RubyLex#check_code_state / IRB::Color.colorize_code, so probe the
	// actual methods and skip when they are missing.
	// IRB::Color.colorize_code (and some RubyLex details) are version-sensitive;
	// this port targets the irb shipped with Ruby >= 4.0. Older bundled IRBs
	// (e.g. the 1.14.x in Ruby 3.4 CI images, or macOS system ruby 2.6) load the
	// files but emit different colour/lex output, so gate the live oracle on the
	// Ruby version. CI lanes below 4.0 skip it and the committed golden corpora
	// (which hold 100% coverage on their own) remain the correctness gate.
	probe := `require 'irb'; require 'irb/ruby-lex'; require 'irb/color'
raise unless RUBY_VERSION >= "4.0"
raise unless IRB.const_defined?(:RubyLex)
raise unless IRB::RubyLex.instance_method(:check_code_state)
raise unless IRB::RubyLex.instance_method(:process_indent_level)
raise unless IRB::Color.respond_to?(:colorize_code)
raise unless defined?(IRB::NestingParser)`
	if err := exec.Command(path, "-e", probe).Run(); err != nil {
		t.Skip("modern irb API not available; skipping MRI oracle")
	}
	return path
}

// rubyContinuation returns IRB's [completeness-terminated, indent, ltype] for
// each snippet, computed by the real RubyLex.
func rubyContinuation(t *testing.T, bin string, corpus []string) []string {
	t.Helper()
	script := `$stdout.binmode
require "irb"; require "irb/ruby-lex"
lex = IRB::RubyLex.new
data = $stdin.read.split("\x00", -1)
data.pop
data.each do |s|
  begin
    _t, opens, term = lex.check_code_state(s, local_variables: [])
    ind = lex.calc_indent_level(opens)
    lt = lex.ltype_from_open_tokens(opens) || ""
    printf("%s|%d|%s\x00", term ? "complete" : "more", ind, lt)
  rescue => e
    printf("ERR:%s\x00", e.class)
  end
end`
	cmd := exec.Command(bin, "-e", script)
	cmd.Stdin = strings.NewReader(strings.Join(corpus, "\x00") + "\x00")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ruby oracle failed: %v\n%s", err, out)
	}
	parts := strings.Split(string(out), "\x00")
	parts = parts[:len(parts)-1]
	if len(parts) != len(corpus) {
		t.Fatalf("oracle returned %d results, want %d\n%s", len(parts), len(corpus), out)
	}
	return parts
}

func TestOracleContinuation(t *testing.T) {
	bin := rubyBin(t)
	got := rubyContinuation(t, bin, continuationCorpus)
	for i, s := range continuationCorpus {
		comp, opens := CheckCode(s)
		goTerm := "complete"
		if comp == More {
			goTerm = "more"
		}
		line := goTerm + "|" + strconv.Itoa(CalcIndentLevel(opens)) + "|" + LtypeFromOpenTokens(opens)
		if line != got[i] {
			t.Errorf("snippet %q:\n  go  = %q\n  ruby= %q", s, line, got[i])
		}
	}
}

func TestOracleColorize(t *testing.T) {
	bin := rubyBin(t)
	script := `$stdout.binmode
require "irb"; require "irb/color"
data = $stdin.read.split("\x00", -1)
data.pop
data.each do |s|
  out = IRB::Color.colorize_code(s, colorable: true)
  $stdout.write(out)
  $stdout.write("\x00")
end`
	cmd := exec.Command(bin, "-e", script)
	cmd.Stdin = strings.NewReader(strings.Join(colorCorpus, "\x00") + "\x00")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ruby colorize oracle failed: %v\n%s", err, out)
	}
	parts := strings.Split(string(out), "\x00")
	parts = parts[:len(parts)-1]
	if len(parts) != len(colorCorpus) {
		t.Fatalf("colorize oracle returned %d, want %d", len(parts), len(colorCorpus))
	}
	for i, s := range colorCorpus {
		got := ColorizeCode(s, true, true)
		if got != parts[i] {
			t.Errorf("colorize %q:\n  go  = %q\n  ruby= %q", s, got, parts[i])
		}
	}
}

func TestOracleAutoIndent(t *testing.T) {
	bin := rubyBin(t)
	script := `$stdout.binmode
require "irb"; require "irb/ruby-lex"
recs = $stdin.read.split("\x00", -1)
recs.pop
recs.each_slice(3) do |code, li, nl|
  lex = IRB::RubyLex.new
  lines = code.split("\n", -1)
  tokens = IRB::RubyLex.ripper_lex_without_warning(code)
  v = lex.process_indent_level(tokens, lines, li.to_i, nl == "1")
  printf("%d\x00", v)
end`
	var in strings.Builder
	for _, c := range indentCorpus {
		in.WriteString(c.Code)
		in.WriteByte(0)
		in.WriteString(strconv.Itoa(c.LineIndex))
		in.WriteByte(0)
		if c.IsNewline {
			in.WriteString("1")
		} else {
			in.WriteString("0")
		}
		in.WriteByte(0)
	}
	cmd := exec.Command(bin, "-e", script)
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("auto-indent oracle failed: %v\n%s", err, out)
	}
	parts := strings.Split(string(out), "\x00")
	parts = parts[:len(parts)-1]
	if len(parts) != len(indentCorpus) {
		t.Fatalf("auto-indent oracle returned %d, want %d", len(parts), len(indentCorpus))
	}
	for i, c := range indentCorpus {
		lines := strings.Split(c.Code, "\n")
		got := AutoIndent(lines, c.LineIndex, c.IsNewline)
		if strconv.Itoa(got) != parts[i] {
			t.Errorf("auto-indent %q line=%d:\n  go  = %d\n  ruby= %s", c.Code, c.LineIndex, got, parts[i])
		}
	}
}

// TestOracleEnvSanity is a cheap guard that the oracle harness itself works (so
// a silently-broken ruby invocation can't make the oracle vacuously pass).
func TestOracleEnvSanity(t *testing.T) {
	bin := rubyBin(t)
	out, err := exec.Command(bin, "-e", "print IRB::VERSION", "-rirb").CombinedOutput()
	if err != nil {
		t.Fatalf("ruby version probe failed: %v\n%s", err, out)
	}
	if len(out) == 0 {
		t.Fatal("empty IRB::VERSION")
	}
	if os.Getenv("IRB_ORACLE_VERBOSE") != "" {
		t.Logf("MRI IRB::VERSION = %s", out)
	}
}
