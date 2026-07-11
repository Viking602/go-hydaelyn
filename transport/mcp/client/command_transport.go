package mcpclient

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type commandIOTransport struct {
	command           *exec.Cmd
	terminateDuration time.Duration
	limitState        *inboundLimitState
	mu                sync.Mutex
	resource          *commandResource
}

func newCommandIOTransport(command *exec.Cmd, terminateDuration time.Duration) *commandIOTransport {
	return &commandIOTransport{
		command:           command,
		terminateDuration: terminateDuration,
		limitState:        newInboundLimitState(),
	}
}

func (t *commandIOTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return nil, errors.Join(err, closePipe(stdoutWrite), closePipe(stdoutRead))
	}
	t.command.Stdout = stdoutWrite
	t.command.Stdin = stdinRead
	if err := t.command.Start(); err != nil {
		return nil, errors.Join(
			err,
			closePipe(stdinWrite),
			closePipe(stdinRead),
			closePipe(stdoutWrite),
			closePipe(stdoutRead),
		)
	}
	childSideErr := errors.Join(closePipe(stdoutWrite), closePipe(stdinRead))
	resource := newCommandResource(t, stdoutRead, stdinWrite)
	t.mu.Lock()
	t.resource = resource
	t.mu.Unlock()
	if childSideErr != nil {
		return nil, errors.Join(childSideErr, resource.Close())
	}
	reader := newNDJSONLimitReadCloser(noCloseReader{Reader: stdoutRead}, maxInboundMessageBytes, t.limitState)
	return (&sdkmcp.IOTransport{Reader: reader, Writer: commandWriter{resource}}).Connect(ctx)
}

func (t *commandIOTransport) Close() error {
	t.mu.Lock()
	resource := t.resource
	t.mu.Unlock()
	if resource == nil {
		return nil
	}
	return resource.Close()
}

func (t *commandIOTransport) inboundLimitError() error { return t.limitState.Err() }

type noCloseReader struct {
	io.Reader
}

func (noCloseReader) Close() error { return nil }

type commandWriter struct {
	resource *commandResource
}

func (w commandWriter) Write(payload []byte) (int, error) {
	return w.resource.stdin.Write(payload)
}

func (w commandWriter) Close() error { return w.resource.Close() }

type commandResource struct {
	command           *exec.Cmd
	stdout            io.ReadCloser
	stdin             io.WriteCloser
	terminateDuration time.Duration
	waitResult        <-chan error
	closeOnce         sync.Once
	closeErr          error
}

func newCommandResource(transport *commandIOTransport, stdout io.ReadCloser, stdin io.WriteCloser) *commandResource {
	waitResult := make(chan error, 1)
	go func() { waitResult <- transport.command.Wait() }()
	return &commandResource{
		command:           transport.command,
		stdout:            stdout,
		stdin:             stdin,
		terminateDuration: transport.terminateDuration,
		waitResult:        waitResult,
	}
}

func (r *commandResource) Close() error {
	r.closeOnce.Do(func() {
		stdinErr := closePipe(r.stdin)
		exited, waitErr := waitCommand(r.waitResult, r.terminateDuration)
		var interruptErr error
		if !exited {
			interruptErr = r.command.Process.Signal(os.Interrupt)
			exited, waitErr = waitCommand(r.waitResult, r.terminateDuration)
		}
		var killErr error
		if !exited {
			killErr = r.command.Process.Kill()
			waitErr = <-r.waitResult
		}
		stdoutErr := closePipe(r.stdout)
		r.closeErr = errors.Join(stdinErr, interruptErr, killErr, waitErr, stdoutErr)
	})
	return r.closeErr
}

func closePipe(closer io.Closer) error {
	err := closer.Close()
	if errors.Is(err, os.ErrClosed) {
		return nil
	}
	return err
}

func waitCommand(result <-chan error, timeout time.Duration) (bool, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return true, err
	case <-timer.C:
		return false, nil
	}
}
