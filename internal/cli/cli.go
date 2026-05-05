// Package cli implements the hydaelyn binary. v2.0 ships a deliberately
// minimal CLI: the framework is library-first, so the CLI only exposes
// utilities for inspecting event logs emitted by a hydaelyn Runner.
//
// The richer recipe / eval / pattern-driven CLI from v1 lives on the
// archive/legacy-v1 branch.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Version is overridden at link time via -ldflags "-X .../cli.Version=v2.0.0".
var Version = "v2.0.0-dev"

func Execute(ctx context.Context, args []string, stdout io.Writer, _ io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing command — try `hydaelyn help`")
	}
	switch args[0] {
	case "help", "-h", "--help":
		return runHelp(stdout)
	case "version", "-v", "--version":
		_, err := fmt.Fprintln(stdout, Version)
		return err
	case "inspect-events":
		return runInspectEvents(ctx, args[1:], stdout)
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runHelp(stdout io.Writer) error {
	_, err := fmt.Fprint(stdout, `hydaelyn — multi-agent orchestrator runtime (v2.0)

Usage:
  hydaelyn version
  hydaelyn inspect-events --events PATH [--task TASKID]
  hydaelyn help
`)
	return err
}
