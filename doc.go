// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package irb is a pure-Go (CGO=0) reimplementation of the deterministic,
// interpreter-independent core of Ruby's IRB — the REPL-support machinery that
// does not need a live Ruby interpreter to run.
//
// IRB's job splits cleanly in two. One half is pure computation over source
// text: deciding whether the lines typed so far form a complete statement or
// need more input, computing the nesting / indent level that drives the
// `irb(main):001:1*` continuation prompt, expanding the prompt %-specs,
// formatting the `=> value` result line, and colourising a token stream. None of
// that needs to *evaluate* anything. The other half — actually evaluating the
// code, reading from the terminal, and persisting the history file — is
// inherently tied to the running interpreter and the host. This package
// implements the first half faithfully and leaves the second to the host (the
// go-embedded-ruby runtime, `rbgo`).
//
// # What lives here
//
//   - Lex turns Ruby source into a Ripper-faithful token stream (events + lexer
//     states), the substrate IRB's RubyLex, NestingParser and Color all build on.
//   - CheckCode is the RubyLex input analysis: it returns Complete / More /
//     SyntaxError for accumulated input, the heart of the multi-line prompt.
//   - OpenTokens / ScanOpens / ParseByLine port IRB::NestingParser's tracking of
//     open syntactic constructs.
//   - CalcIndentLevel, LtypeFromOpenTokens and AutoIndent port the nesting-depth,
//     literal-type and next-line auto-indent calculations.
//   - The PromptMode table, FormatPrompt and GeneratePrompt port IRB's prompt
//     selection and %-spec expansion (:DEFAULT / :SIMPLE / :CLASSIC / :INF_RUBY /
//     :NULL).
//   - ColorizeCode and Colorize port IRB::Color's token→ANSI-SGR mapping.
//   - FormatResult and TruncateResult port the deterministic result-formatting
//     of IRB::Inspector / Irb#output_value (the inspected-string rendering of a
//     live object stays a host seam).
//   - History plus DecodeHistoryLines / EncodeHistoryLines / TrimHistory port the
//     in-memory history list and its on-disk multi-line encoding (the file I/O
//     itself is a host seam).
//   - Commands, LookupCommand and ParseInput port IRB's command registry and the
//     command-vs-expression dispatch decision (executing a command is a host
//     seam).
//
// # Host seams (left to rbgo / the runtime)
//
//   - Evaluation. This package never runs Ruby; it only decides *when* a
//     statement is ready and *how* to render its already-computed inspection.
//   - Terminal I/O. Reading a line, readline/Reline, tty detection and the
//     $stdout.tty? colour gating are the host's job; colour functions take a
//     `colorable` flag instead of probing a terminal.
//   - History file I/O. Loading and saving the history file (paths, mtime
//     checks, permissions) is the host's job; this package only models the list
//     and its encoding.
//   - Object inspection. Producing the inspected string for a value (#inspect,
//     pp, Marshal.dump, YAML.dump) needs the interpreter; ResolveInspector only
//     resolves the requested inspector name.
//
// Faithfulness is validated differentially against the real `irb` gem (MRI):
// the oracle tests run RubyLex / Color / process_indent_level over shared
// corpora and assert byte-for-byte agreement, and committed golden files record
// those decisions so the deterministic, ruby-free test suite holds 100% coverage
// on every platform and architecture.
package irb
