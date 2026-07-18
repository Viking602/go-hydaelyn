package mcpclient

import (
	"context"
	"os"
	"os/exec"
	"time"
)

const defaultStdioCloseTimeout = 5 * time.Second

// StdioConfig describes an MCP server process launched over stdin/stdout.
type StdioConfig struct {
	Command string
	Args    []string
	Dir     string
	// Env is the complete child process environment. By default it replaces the
	// parent environment, including when empty.
	Env []string
	// InheritEnv prepends os.Environ before Env for trusted subprocesses.
	InheritEnv bool
}

// DialStdio configures a stdio client. Initialize starts the subprocess and
// completes the MCP handshake.
func DialStdio(ctx context.Context, cfg StdioConfig) (*Client, error) {
	return DialStdioWithOptions(ctx, cfg, Options{})
}

// DialStdioWithOptions configures a stdio client with optional MCP capability handlers.
func DialStdioWithOptions(ctx context.Context, cfg StdioConfig, options Options) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = cfg.Dir
	if cfg.InheritEnv {
		cmd.Env = append(os.Environ(), cfg.Env...)
	} else {
		cmd.Env = append(make([]string, 0, len(cfg.Env)), cfg.Env...)
	}
	return NewWithOptions(newCommandIOTransport(cmd, defaultStdioCloseTimeout), options), nil
}
