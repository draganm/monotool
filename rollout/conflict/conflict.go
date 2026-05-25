package conflict

import (
	"errors"
	"fmt"
	"io"
	"sort"
)

// Reason describes why a file is in conflict.
type Reason int

const (
	// ReasonUnmarked means the file exists in targetPath but has no monotool
	// marker — it was not last written by monotool.
	ReasonUnmarked Reason = iota
	// ReasonHashMismatch means the file has a marker but its current body's
	// hash does not match the marker hash — it was edited since monotool last
	// wrote it.
	ReasonHashMismatch
)

func (r Reason) String() string {
	switch r {
	case ReasonUnmarked:
		return "unmarked"
	case ReasonHashMismatch:
		return "hash-mismatch"
	default:
		return "unknown"
	}
}

// Conflict is a single in-flight conflict detected during a rollout.
type Conflict struct {
	Path   string
	Reason Reason
}

// Set accumulates conflicts across the rollout flow.
type Set struct {
	items []Conflict
}

// New returns an empty Set.
func New() *Set { return &Set{} }

// Add appends a conflict.
func (s *Set) Add(path string, reason Reason) {
	s.items = append(s.items, Conflict{Path: path, Reason: reason})
}

// Empty reports whether the set has zero conflicts.
func (s *Set) Empty() bool { return len(s.items) == 0 }

// ErrConflicts is the sentinel returned by Err when conflicts were collected.
var ErrConflicts = errors.New("rollout aborted: file ownership conflicts")

// Err returns ErrConflicts when the set is non-empty, otherwise nil.
func (s *Set) Err() error {
	if s.Empty() {
		return nil
	}
	return ErrConflicts
}

// Items returns a copy of the collected conflicts.
func (s *Set) Items() []Conflict {
	out := make([]Conflict, len(s.items))
	copy(out, s.items)
	return out
}

// Report writes a human-readable conflict report to w, grouped by reason with
// paths sorted within each group. Reasons appear in fixed order: unmarked,
// then hash-mismatch.
func (s *Set) Report(w io.Writer) {
	byReason := map[Reason][]string{}
	for _, c := range s.items {
		byReason[c.Reason] = append(byReason[c.Reason], c.Path)
	}

	fmt.Fprintf(w, "rollout aborted: %d conflict(s) detected\n\n", len(s.items))

	for _, r := range []Reason{ReasonUnmarked, ReasonHashMismatch} {
		paths := byReason[r]
		if len(paths) == 0 {
			continue
		}
		sort.Strings(paths)
		switch r {
		case ReasonUnmarked:
			fmt.Fprintln(w, "unmarked (not owned by monotool):")
		case ReasonHashMismatch:
			fmt.Fprintln(w, "hash-mismatch (edited since last rollout):")
		}
		for _, p := range paths {
			fmt.Fprintf(w, "  %s\n", p)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "re-run with --force to overwrite, or resolve manually and commit before retrying.")
}
