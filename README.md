<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-irb/brand/main/social/go-ruby-irb-irb.png" alt="go-ruby-irb/irb" width="720"></p>

# irb — go-ruby-irb

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-irb.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of the deterministic core of Ruby's
[`irb`](https://docs.ruby-lang.org/en/master/IRB.html) REPL-support gem** — the
input-analysis, prompt, indent, colorize, result-formatting, history and command
machinery that drives an IRB session **without any Ruby runtime**.

It is the IRB backend for
[go-embedded-ruby](https://github.com/go-embedded-ruby/ruby), but is a
**standalone, reusable** module — a sibling of
[go-ruby-regexp](https://github.com/go-ruby-regexp/regexp) (the Onigmo engine),
[go-ruby-erb](https://github.com/go-ruby-erb/erb) (the ERB compiler) and
[go-ruby-yaml](https://github.com/go-ruby-yaml/yaml) (the Psych backend).

> **What it is — and isn't.** IRB's evaluable core is *input analysis and
> formatting*: deciding whether the lines typed so far are a complete statement,
> computing the nesting/indent that drives the `irb(main):001:1*` continuation
> prompt, expanding the prompt specs, colourising tokens, and shaping the
> `=> value` line. That is fully deterministic and needs **no interpreter**, so
> it lives here as pure Go. **Evaluation, terminal I/O and history-file I/O stay
> host seams** for the runtime ([`rbgo`](https://github.com/go-embedded-ruby/ruby))
> — this library hands back explicit decisions and strings the host acts on.

## Features

A faithful port of IRB's deterministic pieces, validated against the `irb` gem
on every supported platform:

- **Input analysis (`RubyLex`)** — `CheckCode` returns `Complete` / `More` /
  `SyntaxError` for the accumulated input, reproducing IRB's continuation
  decision for open `def`/`do`/`if`/`{`/`[`/`(`, unterminated strings, regexps,
  heredocs and word-lists, trailing operators / `.` / `\`, endless ranges and
  stray `end`.
- **Nesting (`NestingParser`)** — `OpenTokens` / `ScanOpens` / `ParseByLine`
  track the stack of open constructs (method heads, lambda heads, for/while
  conditions, alias/undef, heredocs, embdocs, every bracket and literal).
- **Prompt** — the `:DEFAULT` / `:SIMPLE` / `:CLASSIC` / `:INF_RUBY` / `:NULL`
  modes plus `%N`/`%m`/`%M`/`%l`/`%i`/`%n`/`%%` spec expansion, main-object
  truncation, and the `GeneratePrompt` I/S/C selection with auto-indent padding.
- **Auto-indent** — `AutoIndent` ports `process_indent_level`, including the
  free-indent (string/regexp/symbol), heredoc (squiggly/dashed/plain) and
  `=begin`/`=end` embdoc special cases.
- **Colorize (`IRB::Color`)** — `ColorizeCode` maps the token stream to ANSI SGR
  sequences exactly as IRB does (the symbol-state machine, keyword/const
  re-colouring, per-line reset).
- **Result formatting** — `FormatResult` applies the `:RETURN` template and
  `TruncateResult` ports the single-page `echo_on_assignment: :truncate` cap.
- **History model** — an in-memory `History` plus the `\`-continuation
  encode/decode and entry-count trimming used on disk.
- **Commands** — the full built-in command/alias/category registry and the
  `ParseInput` command-vs-expression dispatch decision.

## Host seams left for rbgo

| Concern | Why it is a seam |
| --- | --- |
| **Evaluation** | This package decides *when* a statement is ready, never runs it. |
| **Terminal I/O** | readline/Reline, tty detection, `$stdout.tty?` colour gating. |
| **History file I/O** | paths, mtime checks, permissions — only the list/encoding is here. |
| **Object inspection** | `#inspect`/`pp`/`Marshal`/`YAML` need the interpreter. |

## Usage

```go
import "github.com/go-ruby-irb/irb"

// Multi-line continuation: keep reading until the statement is complete.
verdict, opens := irb.CheckCode("def greet\n  puts :hi")
// verdict == irb.More, opens describes the open `def`.

prompt := irb.GeneratePrompt(
    irb.PromptDefault,
    irb.PromptContext{IRBName: "irb", Main: "main"},
    opens, verdict == irb.More, /*lineNo=*/2, /*prompting=*/true, /*autoIndent=*/false,
)
// prompt == "irb(main):002* "

colored := irb.ColorizeCode(`puts "hi #{name}"`, true, /*colorable=*/true)
line := irb.FormatResult("=> %s\n", `"hi there"`) // "=> \"hi there\"\n"
```

## Tests & coverage

The suite is **100%-covered** and runs on three OSes and all six 64-bit Go
architectures. Differential **oracle** tests run the real `irb` gem (MRI) over
shared corpora and assert byte-for-byte agreement on continuation, indent,
ltype, auto-indent and colorize; committed **golden** files record those
decisions so the deterministic, ruby-free suite holds 100% where ruby is absent
(the Windows lane and the qemu cross-arch lanes).

```sh
GOWORK=off CGO_ENABLED=0 go test -race ./...
```

To regenerate the golden files (only after the oracle is green against a modern
`irb`):

```sh
IRB_GEN_GOLDEN=1 go test -run TestGenGolden
```

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright (c) the go-ruby-irb/irb authors.
