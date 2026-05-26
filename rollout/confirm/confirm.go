package confirm

import (
	"errors"
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
