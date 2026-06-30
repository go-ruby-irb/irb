// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import "strings"

// This file ports IRB's command model: the registry of built-in commands (with
// their aliases and categories), and the IRB::Context#parse_input dispatch logic
// that decides whether a line of input is a command invocation or a Ruby
// expression. Running a command's effect (printing source, listing methods,
// changing the workspace) needs the live session, so it is a host seam; what
// lives here is the deterministic *parsing* and lookup.

// CommandSpec describes one built-in IRB command.
type CommandSpec struct {
	Name     string
	Category string
	Aliases  []string
}

// Commands is the built-in command table, mirroring IRB::Command.commands plus
// the default aliases registered in IRB::Command::DefaultCommands.
var Commands = []CommandSpec{
	{Name: "cd", Category: "Workspace", Aliases: []string{}},
	{Name: "copy", Category: "Misc", Aliases: []string{}},
	{Name: "irb", Category: "Multi-irb (DEPRECATED)", Aliases: []string{}},
	{Name: "irb_backtrace", Category: "Debugging", Aliases: []string{"backtrace", "bt"}},
	{Name: "irb_break", Category: "Debugging", Aliases: []string{"break"}},
	{Name: "irb_catch", Category: "Debugging", Aliases: []string{"catch"}},
	{Name: "irb_change_workspace", Category: "Workspace", Aliases: []string{"chws", "cws", "irb_chws", "irb_cws", "irb_change_binding", "irb_cb", "cb"}},
	{Name: "irb_context", Category: "IRB", Aliases: []string{"context"}},
	{Name: "irb_continue", Category: "Debugging", Aliases: []string{"continue"}},
	{Name: "irb_current_working_workspace", Category: "Workspace", Aliases: []string{"cwws", "pwws", "irb_print_working_workspace", "irb_cwws", "irb_pwws", "irb_current_working_binding", "irb_print_working_binding", "irb_cwb", "irb_pwb"}},
	{Name: "irb_debug", Category: "Debugging", Aliases: []string{"debug"}},
	{Name: "irb_debug_info", Category: "Debugging", Aliases: []string{"info"}},
	{Name: "irb_delete", Category: "Debugging", Aliases: []string{"delete"}},
	{Name: "irb_disable_irb", Category: "IRB", Aliases: []string{"disable_irb"}},
	{Name: "irb_edit", Category: "Misc", Aliases: []string{"edit"}},
	{Name: "irb_exit", Category: "IRB", Aliases: []string{"exit", "quit", "irb_quit"}},
	{Name: "irb_exit!", Category: "IRB", Aliases: []string{"exit!"}},
	{Name: "irb_fg", Category: "Multi-irb (DEPRECATED)", Aliases: []string{"fg"}},
	{Name: "irb_finish", Category: "Debugging", Aliases: []string{"finish"}},
	{Name: "irb_help", Category: "Help", Aliases: []string{"help", "show_cmds"}},
	{Name: "irb_history", Category: "IRB", Aliases: []string{"history", "hist"}},
	{Name: "irb_info", Category: "IRB", Aliases: []string{}},
	{Name: "irb_jobs", Category: "Multi-irb (DEPRECATED)", Aliases: []string{"jobs"}},
	{Name: "irb_kill", Category: "Multi-irb (DEPRECATED)", Aliases: []string{"kill"}},
	{Name: "irb_load", Category: "IRB", Aliases: []string{}},
	{Name: "irb_ls", Category: "Context", Aliases: []string{"ls"}},
	{Name: "irb_measure", Category: "Misc", Aliases: []string{"measure"}},
	{Name: "irb_next", Category: "Debugging", Aliases: []string{"next"}},
	{Name: "irb_pop_workspace", Category: "Workspace", Aliases: []string{"popws", "irb_popws", "irb_pop_binding", "irb_popb", "popb"}},
	{Name: "irb_push_workspace", Category: "Workspace", Aliases: []string{"pushws", "irb_pushws", "irb_push_binding", "irb_pushb", "pushb"}},
	{Name: "irb_require", Category: "IRB", Aliases: []string{}},
	{Name: "irb_show_doc", Category: "Context", Aliases: []string{"show_doc", "ri"}},
	{Name: "irb_show_source", Category: "Context", Aliases: []string{"show_source"}},
	{Name: "irb_source", Category: "IRB", Aliases: []string{"source"}},
	{Name: "irb_step", Category: "Debugging", Aliases: []string{"step"}},
	{Name: "irb_whereami", Category: "Context", Aliases: []string{"whereami"}},
	{Name: "irb_workspaces", Category: "Workspace", Aliases: []string{"workspaces", "irb_bindings", "bindings"}},
}

// commandIndex maps every canonical name and alias to its canonical command.
var commandIndex = buildCommandIndex()

func buildCommandIndex() map[string]string {
	m := make(map[string]string)
	for _, c := range Commands {
		m[c.Name] = c.Name
		for _, a := range c.Aliases {
			m[a] = c.Name
		}
	}
	return m
}

// LookupCommand resolves a command name or alias to its canonical name.
func LookupCommand(name string) (string, bool) {
	c, ok := commandIndex[name]
	return c, ok
}

// CommandNames returns all canonical command names (sorted by insertion order of
// the table).
func CommandNames() []string {
	out := make([]string, len(Commands))
	for i, c := range Commands {
		out[i] = c.Name
	}
	return out
}

// assignOperators mirrors IRB's ASSIGN_OPERATORS_REGEXP set; a command argument
// starting with one of these (other than == or =~) means the line is an
// assignment, not a command.
var assignOperators = []string{"<<=", ">>=", "**=", "&&=", "||=", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "="}

// ParsedInput is the result of analysing one input line: either a recognised
// command invocation or a plain Ruby expression.
type ParsedInput struct {
	IsCommand bool
	Command   string // canonical command name when IsCommand
	Arg       string // the argument text after the command name
	Code      string // the original code (always set)
}

// ParseInput ports IRB::Context#parse_input's command-vs-expression decision.
// localVariables is the set of in-scope local variable names (a name that is a
// local variable shadows a command), and isAssignment reports whether the parser
// already classified the code as an assignment expression (assignments are never
// commands). A line is a command only when it is a single line, names a known
// command, is not shadowed by a local, and is not an assignment.
func ParseInput(code string, localVariables map[string]bool, isAssignment bool) ParsedInput {
	res := ParsedInput{Code: code}
	stripped := strings.TrimSpace(code)
	name, arg := splitCommand(stripped)

	multiline := strings.Count(strings.TrimRight(code, "\n"), "\n") > 0
	shadowed := localVariables != nil && localVariables[name]
	argAssign := startsWithAssign(arg)

	if multiline || name == "" || shadowed || isAssignment || argAssign {
		return res
	}
	canonical, ok := LookupCommand(name)
	if !ok {
		return res
	}
	res.IsCommand = true
	res.Command = canonical
	res.Arg = arg
	return res
}

// splitCommand splits a stripped line into the command name and the remaining
// argument, on the first run of whitespace (Ruby's split(/\s+/, 2)).
func splitCommand(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			name := s[:i]
			arg := strings.TrimLeft(s[i:], " \t")
			return name, arg
		}
	}
	return s, ""
}

// startsWithAssign reports whether arg begins with an assignment operator other
// than `==` or `=~`, matching IRB's `arg.start_with?(ASSIGN_OPERATORS_REGEXP) &&
// !arg.start_with?(/==|=~/)`.
func startsWithAssign(arg string) bool {
	if strings.HasPrefix(arg, "==") || strings.HasPrefix(arg, "=~") {
		return false
	}
	for _, op := range assignOperators {
		if strings.HasPrefix(arg, op) {
			return true
		}
	}
	return false
}
