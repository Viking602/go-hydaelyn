package host

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/tool"
)

type capabilityProviderDriver struct {
	name     string
	metadata provider.Metadata
	invoker  *capability.Invoker
	recorder capability.PolicyOutcomeRecorder
}

func (d capabilityProviderDriver) Metadata() provider.Metadata {
	return d.metadata
}

func (d capabilityProviderDriver) Stream(ctx context.Context, request provider.Request) (provider.Stream, error) {
	ctx = capability.WithPolicyOutcomeRecorder(ctx, d.recorder)
	result, err := d.invoker.Invoke(ctx, capability.Call{
		Type:     capability.TypeLLM,
		Name:     d.name,
		Input:    request,
		Metadata: cloneStringMap(request.Metadata),
	})
	if err != nil {
		return nil, err
	}
	events, ok := result.Output.([]provider.Event)
	if !ok {
		return nil, fmt.Errorf("capability provider returned unexpected output type %T", result.Output)
	}
	return provider.NewSliceStream(events), nil
}

type capabilityToolDriver struct {
	definition tool.Definition
	invoker    *capability.Invoker
	recorder   capability.PolicyOutcomeRecorder
}

func (d capabilityToolDriver) Definition() tool.Definition {
	return d.definition
}

func (d capabilityToolDriver) Execute(ctx context.Context, call tool.Call, sink tool.UpdateSink) (tool.Result, error) {
	ctx = capability.WithPolicyOutcomeRecorder(ctx, d.recorder)
	result, err := d.invoker.Invoke(ctx, capability.Call{
		Type: capability.TypeTool,
		Name: d.definition.Name,
		Input: toolCapabilityInput{
			Call: call,
			Sink: sink,
		},
		Permissions: capabilityPermissionsForDefinition(d.definition),
		Metadata:    cloneStringMap(d.definition.Metadata),
	})
	if err != nil {
		return tool.Result{}, err
	}
	item, ok := result.Output.(tool.Result)
	if !ok {
		return tool.Result{}, fmt.Errorf("capability tool returned unexpected output type %T", result.Output)
	}
	return item, nil
}

type toolCapabilityInput struct {
	Call tool.Call
	Sink tool.UpdateSink
}

func capabilityPermissionsForDefinition(definition tool.Definition) []capability.Permission {
	raw := strings.TrimSpace(definition.Metadata["permission"])
	if raw == "" {
		return nil
	}
	items := strings.Split(raw, ",")
	permissions := make([]capability.Permission, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		permissions = append(permissions, capability.Permission{Name: name})
	}
	return permissions
}

func providerCapabilityHandler(driver provider.Driver) capability.Handler {
	return func(ctx context.Context, call capability.Call) (capability.Result, error) {
		request, ok := call.Input.(provider.Request)
		if !ok {
			return capability.Result{}, fmt.Errorf("expected provider.Request input, got %T", call.Input)
		}
		stream, err := driver.Stream(ctx, request)
		if err != nil {
			return capability.Result{}, err
		}
		defer stream.Close()
		events := make([]provider.Event, 0, 8)
		usage := capability.Usage{}
		for {
			event, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					break
				}
				return capability.Result{}, err
			}
			events = append(events, event)
			if event.Kind == provider.EventDone {
				usage = capability.Usage{
					InputTokens:  event.Usage.InputTokens,
					OutputTokens: event.Usage.OutputTokens,
					TotalTokens:  event.Usage.TotalTokens,
					StopReason:   string(event.StopReason),
				}
			}
		}
		return capability.Result{
			Output: events,
			Usage:  usage,
			Metadata: map[string]string{
				"provider": driver.Metadata().Name,
			},
		}, nil
	}
}

func toolCapabilityHandler(driver tool.Driver) capability.Handler {
	return func(ctx context.Context, call capability.Call) (capability.Result, error) {
		input, ok := call.Input.(toolCapabilityInput)
		if !ok {
			return capability.Result{}, fmt.Errorf("expected toolCapabilityInput, got %T", call.Input)
		}
		result, err := driver.Execute(ctx, input.Call, input.Sink)
		if err != nil {
			return capability.Result{}, err
		}
		return capability.Result{
			Output: result,
			Metadata: map[string]string{
				"tool": result.Name,
			},
		}, nil
	}
}
