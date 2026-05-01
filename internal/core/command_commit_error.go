package core

import "errors"

type commitCommandError struct {
	err error
}

func (e commitCommandError) Error() string { return e.err.Error() }
func (e commitCommandError) Unwrap() error { return e.err }

func commitWithError(err error) error {
	if err == nil {
		return nil
	}
	return commitCommandError{err: err}
}

func isCommitCommandError(err error) bool {
	var target commitCommandError
	return errors.As(err, &target)
}
