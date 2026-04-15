package toolkit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"

	"hydaelyn/tool"
)

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
		definition: tool.Definition{
			Name:        name,
			Description: config.description,
			InputSchema: schema,
			Tags:        config.tags,
			Metadata:    config.metadata,
			Origin:      "http",
		},
		execute: func(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
			client := cfg.Client
			if client == nil {
				client = &http.Client{}
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
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
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
		definition: tool.Definition{
			Name:        name,
			Description: config.description,
			InputSchema: schema,
			Tags:        config.tags,
			Metadata:    config.metadata,
			Origin:      "process",
		},
		execute: func(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
			command := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
			command.Dir = cfg.Dir
			if len(cfg.Env) > 0 {
				command.Env = append(command.Env, cfg.Env...)
			}
			if cfg.StdinJSON {
				command.Stdin = bytes.NewReader(call.Arguments)
			}
			output, err := command.CombinedOutput()
			if err != nil {
				return tool.Result{}, err
			}
			return resultFromPayload(call, output, false), nil
		},
	}
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
