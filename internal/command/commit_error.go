package command

import "errors"

type commitError struct {
	err error
}

func (e commitError) Error() string { return e.err.Error() }
func (e commitError) Unwrap() error { return e.err }

func CommitWithError(err error) error {
	if err == nil {
		return nil
	}
	return commitError{err: err}
}

func IsCommitError(err error) bool {
	var target commitError
	return errors.As(err, &target)
}
