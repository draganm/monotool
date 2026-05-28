package confirm

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Confirmer asks the user whether to perform a destructive action.
// Returns proceed=true to perform the action, proceed=false to skip just
// this one, or a non-nil error to abort the rollout entirely. A nil
// Confirmer means "no prompting, proceed unconditionally".
type Confirmer func(action, path string) (proceed bool, err error)

// ErrAborted is returned by a Confirmer when the user picks the abort
// option. Callers should check via errors.Is and render a friendly message
// rather than a wrapped error chain.
var ErrAborted = errors.New("rollout aborted by user")

// TTYConfirmer returns a Confirmer that prompts on out and reads decisions
// from in line-by-line. The first non-whitespace character of each input
// line is lowercased and mapped: 'y' = proceed, 'n' = skip, 'a' = abort.
// Anything else reprints the prompt. EOF on in aborts.
func TTYConfirmer(in io.Reader, out io.Writer) Confirmer {
	br := bufio.NewReader(in)
	return func(action, path string) (bool, error) {
		for {
			fmt.Fprintf(out, "%s %s? [y/n/a] ", action, path)
			line, err := br.ReadString('\n')
			if err == io.EOF && line == "" {
				return false, ErrAborted
			}
			if err != nil && err != io.EOF {
				return false, fmt.Errorf("read confirmation: %w", err)
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			switch strings.ToLower(trimmed)[0] {
			case 'y':
				return true, nil
			case 'n':
				return false, nil
			case 'a':
				return false, ErrAborted
			}
			// otherwise, loop and reprompt
		}
	}
}
