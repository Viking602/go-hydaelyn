package main

import (
	"os"
	"testing"
)

func TestMainRunsVersionCommand(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"hydaelyn", "version"}
	t.Cleanup(func() { os.Args = originalArgs })

	main()
}
