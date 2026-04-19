package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
)

func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing command")
	}
	handler, ok := commandHandlers(ctx, stdout)[args[0]]
	if !ok {
		return fmt.Errorf("unknown command: %s", args[0])
	}
	return handler(args[1:])
}

func commandHandlers(ctx context.Context, stdout io.Writer) map[string]func([]string) error {
	return map[string]func([]string) error{
		"init":              func(args []string) error { return runInit(args, stdout) },
		"new":               func(args []string) error { return runNew(args, stdout) },
		"run":               func(args []string) error { return runRun(ctx, args, stdout) },
		"validate":          func(args []string) error { return runValidate(args, stdout) },
		"compile":           func(args []string) error { return runCompile(args, stdout) },
		"inspect":           func(args []string) error { return runInspect(args, stdout) },
		"evaluate":          func(args []string) error { return runEvaluate(args, stdout) },
		"replay":            func(args []string) error { return runReplay(args, stdout) },
		"run-deterministic": func(args []string) error { return runDeterministic(ctx, args, stdout) },
	}
}
