package kit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/Viking602/venat/tool"
)

const (
	defaultHTTPTimeout           = 30 * time.Second
	defaultMaxResponseBytes      = 1 << 20
	defaultMaxProcessOutputBytes = 1 << 20
)

var errProcessOutputTooLarge = errors.New("process output too large")

type HTTPToolConfig struct {
	Method  string
	URL     string
	Headers map[string]string
	Client  *http.Client
}

func HTTPTool(name string, schema tool.Schema, cfg HTTPToolConfig, options ...ToolOption) tool.Driver {
	config := toolConfig{origin: "http"}
	for _, option := range options {
		option(&config)
	}
	driver := staticDriver{
		definition: definitionFromConfig(name, schema, config),
		execute: func(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
			client := cfg.Client
			if client == nil {
				client = &http.Client{Timeout: defaultHTTPTimeout}
			}
			method := cfg.Method
			if method == "" {
				method = http.MethodPost
			}
			request, err := http.NewRequestWithContext(ctx, method, cfg.URL, bytes.NewReader(call.Arguments))
			if err != nil {
				return tool.Result{}, err
			}
			request.Header.Set("Content-Type", "application/json")
			for key, value := range cfg.Headers {
				request.Header.Set(key, value)
			}
			response, err := client.Do(request)
			if err != nil {
				return tool.Result{}, err
			}
			defer func() { _ = response.Body.Close() }()
			body, err := readLimited(response.Body, defaultMaxResponseBytes)
			if err != nil {
				return tool.Result{}, err
			}
			return resultFromPayload(call, body, response.StatusCode >= 400), nil
		},
	}
	return driver
}

type ProcessToolConfig struct {
	Command   string
	Args      []string
	Dir       string
	Env       []string
	StdinJSON bool
}

func ProcessTool(name string, schema tool.Schema, cfg ProcessToolConfig, options ...ToolOption) tool.Driver {
	config := toolConfig{origin: "process"}
	for _, option := range options {
		option(&config)
	}
	return staticDriver{
		definition: definitionFromConfig(name, schema, config),
		execute: func(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
			output, err := runProcess(ctx, cfg, call.Arguments)
			if err != nil {
				return tool.Result{}, err
			}
			return resultFromPayload(call, output, false), nil
		},
	}
}

func readLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("http response body too large: limit %d bytes", maxBytes)
	}
	return body, nil
}

func runProcess(ctx context.Context, cfg ProcessToolConfig, input []byte) ([]byte, error) {
	commandCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	command := exec.CommandContext(commandCtx, cfg.Command, cfg.Args...)
	command.Dir = cfg.Dir
	if len(cfg.Env) > 0 {
		command.Env = append(command.Env, cfg.Env...)
	}
	if cfg.StdinJSON {
		command.Stdin = bytes.NewReader(input)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}

	output := &limitedOutputBuffer{max: defaultMaxProcessOutputBytes}
	copyErrs := make(chan error, 2)
	copyOutput := func(reader io.Reader) {
		_, err := io.Copy(output, reader)
		if errors.Is(err, errProcessOutputTooLarge) {
			cancel()
		}
		copyErrs <- err
	}
	go copyOutput(stdout)
	go copyOutput(stderr)

	waitErr := command.Wait()
	stdoutErr := <-copyErrs
	stderrErr := <-copyErrs
	if output.TooLarge() {
		return output.Bytes(), fmt.Errorf("process output too large: limit %d bytes", defaultMaxProcessOutputBytes)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return output.Bytes(), ctxErr
	}
	if waitErr != nil {
		return output.Bytes(), waitErr
	}
	if stdoutErr != nil {
		return output.Bytes(), stdoutErr
	}
	if stderrErr != nil {
		return output.Bytes(), stderrErr
	}
	return output.Bytes(), nil
}

type limitedOutputBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	max      int64
	tooLarge bool
}

func (b *limitedOutputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if int64(b.buf.Len()+len(p)) > b.max {
		remaining := int(b.max) - b.buf.Len()
		if remaining > 0 {
			_, _ = b.buf.Write(p[:remaining])
		}
		b.tooLarge = true
		return len(p), errProcessOutputTooLarge
	}
	return b.buf.Write(p)
}

func (b *limitedOutputBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte{}, b.buf.Bytes()...)
}

func (b *limitedOutputBuffer) TooLarge() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tooLarge
}

type staticDriver struct {
	definition tool.Definition
	execute    func(ctx context.Context, call tool.Call, sink tool.UpdateSink) (tool.Result, error)
}

func (d staticDriver) Definition() tool.Definition {
	return d.definition
}

func (d staticDriver) Execute(ctx context.Context, call tool.Call, sink tool.UpdateSink) (tool.Result, error) {
	return d.execute(ctx, call, sink)
}

func resultFromPayload(call tool.Call, payload []byte, isError bool) tool.Result {
	result := tool.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    string(payload),
		IsError:    isError,
	}
	if json.Valid(payload) {
		result.Structured = append([]byte{}, payload...)
	}
	return result
}
