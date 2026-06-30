// Copyright (c) the go-ruby-irb/irb authors
//
// SPDX-License-Identifier: BSD-3-Clause

package irb

import "strings"

// This file ports the deterministic history-list model from IRB::History /
// IRB::HistorySavingAbility: the in-memory entry list, the multi-line
// "\"-continuation encoding used on disk, and the entry-count trimming. The
// actual file load/save (opening the history file, mtime checks, permissions)
// is a host seam — it needs the filesystem — so this type takes and returns the
// decoded line slices and leaves the I/O to rbgo.

// DefaultEntryLimit is IRB::History::DEFAULT_ENTRY_LIMIT.
const DefaultEntryLimit = 1000

// History is the pure in-memory list of input entries. A multi-line entry is
// stored as a single string containing embedded "\n".
type History struct {
	entries []string
}

// NewHistory returns an empty history list.
func NewHistory() *History { return &History{} }

// Push appends an entry to the history.
func (h *History) Push(entry string) { h.entries = append(h.entries, entry) }

// Entries returns a copy of the current entries.
func (h *History) Entries() []string {
	out := make([]string, len(h.entries))
	copy(out, h.entries)
	return out
}

// Len reports the number of entries.
func (h *History) Len() int { return len(h.entries) }

// At returns the entry at index i (0-based). Negative indices count from the end
// like Ruby's Array#[]. The ok result reports whether the index was in range.
func (h *History) At(i int) (string, bool) {
	if i < 0 {
		i += len(h.entries)
	}
	if i < 0 || i >= len(h.entries) {
		return "", false
	}
	return h.entries[i], true
}

// Clear empties the history.
func (h *History) Clear() { h.entries = nil }

// SaveLimit resolves IRB's SAVE_HISTORY setting (false→0, true→default,
// otherwise the integer) the way IRB::History.save_history does. A negative
// value means "infinite".
func SaveLimit(setting any) int {
	switch v := setting.(type) {
	case bool:
		if v {
			return DefaultEntryLimit
		}
		return 0
	case int:
		return v
	default:
		return 0
	}
}

// DecodeHistoryLines reconstructs entries from on-disk lines using IRB's
// continuation rule: a stored line ending in a backslash continues into the next
// line (the backslash is dropped and replaced by a newline). It ports the
// load_history merge loop. multiline selects the Reline behaviour (the only one
// that merges); when false each line is its own entry.
func DecodeHistoryLines(lines []string, multiline bool) []string {
	var out []string
	for _, l := range lines {
		l = strings.TrimRight(l, "\n")
		if multiline && len(out) > 0 && strings.HasSuffix(out[len(out)-1], "\\") {
			out[len(out)-1] = strings.TrimSuffix(out[len(out)-1], "\\") + "\n" + l
		} else {
			out = append(out, l)
		}
	}
	return out
}

// EncodeHistoryLines renders entries for storage using IRB's save_history rule:
// each entry's embedded newlines become "\\\n" so a multi-line entry round-trips
// through DecodeHistoryLines. It ports `l.scrub.split("\n").join("\\\n")`.
func EncodeHistoryLines(entries []string) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = strings.Join(strings.Split(e, "\n"), "\\\n")
	}
	return out
}

// TrimHistory caps the entry list to the save limit, dropping the oldest
// entries, mirroring `hist.last(save_history)`. A negative (infinite) or
// non-positive-overflow limit returns the entries unchanged.
func TrimHistory(entries []string, limit int) []string {
	if limit < 0 || len(entries) <= limit {
		return entries
	}
	return entries[len(entries)-limit:]
}
