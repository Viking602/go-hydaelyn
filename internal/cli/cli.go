// Package cli implements the deliberately minimal venat binary. The
// framework is library-first, so the CLI only exposes
// utilities for inspecting event logs emitted by a Venat Runner.
//
// The richer recipe / eval / pattern-driven CLI from v1 lives on the
// archive/legacy-v1 branch.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
)

func Execute(ctx context.Context, args []string, stdout io.Writer, _ io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing command — try `venat help`")
	}
	buildInfo, _ := debug.ReadBuildInfo()
	version := resolveBuildVersion(buildInfo)
	switch args[0] {
	case "help", "-h", "--help":
		return runHelp(stdout, version)
	case "version", "-v", "--version":
		_, err := fmt.Fprintln(stdout, version)
		return err
	case "inspect-events":
		return runInspectEvents(ctx, args[1:], stdout)
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func resolveBuildVersion(info *debug.BuildInfo) string {
	// Local builds have no module checksum, even when Go synthesizes a
	// pseudo-version from VCS metadata.
	if info == nil || info.Main.Sum == "" {
		return "devel"
	}
	return info.Main.Version
}

func runHelp(stdout io.Writer, version string) error {
	_, err := fmt.Fprintf(stdout, `venat — multi-agent orchestrator runtime (%s)

Usage:
  venat version
  venat inspect-events --events PATH [--task TASKID]
  venat help
`, version)
	return err
}
