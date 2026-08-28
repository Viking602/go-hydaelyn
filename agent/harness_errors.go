package agent

import "errors"

var (
	ErrLaneBusy          = errors.New("agent: lane busy")
	ErrNothingToResume   = errors.New("agent: nothing to resume")
	ErrHarnessClosed     = errors.New("agent: harness closed")
	ErrHarnessFault      = errors.New("agent: harness fault")
	ErrInvalidMessage    = errors.New("agent: invalid message")
	ErrMissingIdentities = errors.New("agent: missing identities")
	// ErrRegisterMissing marks a lane or operation register that the session
	// invariants require to exist. It only ever surfaces wrapped in
	// ErrHarnessFault: the state is broken, not momentarily unreachable.
	ErrRegisterMissing = errors.New("agent: session register missing")
)
