package core

import commandbus "github.com/Viking602/venat/internal/command"

func commitWithError(err error) error {
	return commandbus.CommitWithError(err)
}

func isCommitCommandError(err error) bool {
	return commandbus.IsCommitError(err)
}
