package kit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/tool"
)

type ToolOption func(*toolConfig)

type toolConfig struct {
	description         string
	tags                []string
	metadata            map[string]string
	origin              string
	requiredPermissions []string
	requiresApproval    bool
	riskLevel           string
}

func Description(description string) ToolOption {
	return func(cfg *toolConfig) {
		cfg.description = description
	}
}

func Tags(tags ...string) ToolOption {
	return func(cfg *toolConfig) {
		cfg.tags = append([]string{}, tags...)
	}
}

func Metadata(metadata map[string]string) ToolOption {
	return func(cfg *toolConfig) {
		cfg.metadata = metadata
	}
}

func Origin(origin string) ToolOption {
	return func(cfg *toolConfig) {
		cfg.origin = origin
	}
}

func RequiredPermissions(permissions ...string) ToolOption {
	return func(cfg *toolConfig) {
		cfg.requiredPermissions = append([]string{}, permissions...)
	}
}

func RequiresApproval() ToolOption {
	return func(cfg *toolConfig) {
		cfg.requiresApproval = true
	}
}

func RiskLevel(level string) ToolOption {
	return func(cfg *toolConfig) {
		cfg.riskLevel = level
	}
}

type BundleSpec struct {
	Tools []tool.Driver
}

func Bundle(items ...any) BundleSpec {
	spec := BundleSpec{}
	for _, item := range items {
		switch current := item.(type) {
		case tool.Driver:
			spec.Tools = append(spec.Tools, current)
		case BundleSpec:
			spec.Tools = append(spec.Tools, current.Tools...)
		}
	}
	return spec
}

type Middleware func(tool.Driver) tool.Driver

func ChainMiddlewares(driver tool.Driver, middlewares ...Middleware) tool.Driver {
	current := driver
	for idx := len(middlewares) - 1; idx >= 0; idx-- {
		current = middlewares[idx](current)
	}
	return current
}

func Tool(name string, fn any, options ...ToolOption) (tool.Driver, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("tool name is required")
	}
	cfg := toolConfig{}
	for _, option := range options {
		option(&cfg)
	}
	builder, err := newFunctionTool(name, fn, cfg)
	if err != nil {
		return nil, err
	}
	return builder, nil
}

type functionTool struct {
	definition tool.Definition
	fn         reflect.Value
	inputType  reflect.Type
	outputType reflect.Type
	wantsCtx   bool
	wantsSink  bool
}

func newFunctionTool(name string, fn any, cfg toolConfig) (*functionTool, error) {
	value := reflect.ValueOf(fn)
	if value.Kind() != reflect.Func {
		return nil, fmt.Errorf("tool %s expects a function", name)
	}
	signature := value.Type()
	wantsCtx := false
	wantsSink := false
	index := 0
	if signature.NumIn() > 0 && signature.In(0) == reflect.TypeOf((*context.Context)(nil)).Elem() {
		wantsCtx = true
		index++
	}
	if signature.NumIn()-index < 1 || signature.NumIn()-index > 2 {
		return nil, fmt.Errorf("tool %s expects func([context.Context], In[, tool.UpdateSink]) (Out, error)", name)
	}
	inputType := signature.In(index)
	if signature.NumIn()-index == 2 {
		if signature.In(index+1) != reflect.TypeOf((tool.UpdateSink)(nil)) {
			return nil, fmt.Errorf("tool %s received unsupported second argument", name)
		}
		wantsSink = true
	}
	if signature.NumOut() != 2 || !signature.Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		return nil, fmt.Errorf("tool %s expects (Out, error) return values", name)
	}
	outputType := signature.Out(0)
	schema, err := schemaFor(inputType)
	if err != nil {
		return nil, err
	}
	return &functionTool{
		definition: tool.Definition{
			Name:        name,
			Description: cfg.description,
			InputSchema: schema,
			Tags:        cfg.tags,
			Metadata:    cfg.metadata,
			Origin:      cfg.origin,
			Security: message.ToolSecurity{
				RequiredPermissions: append([]string{}, cfg.requiredPermissions...),
				RequiresApproval:    cfg.requiresApproval,
				RiskLevel:           cfg.riskLevel,
			},
			RequiredPermissions: append([]string{}, cfg.requiredPermissions...),
			RequiresApproval:    cfg.requiresApproval,
			RiskLevel:           cfg.riskLevel,
		},
		fn:         value,
		inputType:  inputType,
		outputType: outputType,
		wantsCtx:   wantsCtx,
		wantsSink:  wantsSink,
	}, nil
}

func (t *functionTool) Definition() tool.Definition {
	return t.definition
}

func (t *functionTool) Execute(ctx context.Context, call tool.Call, sink tool.UpdateSink) (tool.Result, error) {
	inputValue, err := decodeInput(t.inputType, call.Arguments)
	if err != nil {
		return tool.Result{}, err
	}
	args := make([]reflect.Value, 0, 3)
	if t.wantsCtx {
		args = append(args, reflect.ValueOf(ctx))
	}
	args = append(args, inputValue)
	if t.wantsSink {
		if sink == nil {
			sink = func(tool.Update) error { return nil }
		}
		args = append(args, reflect.ValueOf(sink))
	}
	values := t.fn.Call(args)
	if errValue := values[1].Interface(); errValue != nil {
		return tool.Result{}, errValue.(error)
	}
	output := values[0].Interface()
	structured, err := json.Marshal(output)
	if err != nil {
		return tool.Result{}, err
	}
	content := string(structured)
	if t.outputType.Kind() == reflect.String {
		content = values[0].String()
	}
	return tool.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    content,
		Structured: structured,
	}, nil
}

func decodeInput(target reflect.Type, payload json.RawMessage) (reflect.Value, error) {
	value := reflect.New(target)
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	if target.Kind() != reflect.Struct {
		value = reflect.New(target)
		if err := json.Unmarshal(payload, value.Interface()); err != nil {
			return reflect.Value{}, err
		}
		return value.Elem(), nil
	}
	if err := json.Unmarshal(payload, value.Interface()); err != nil {
		return reflect.Value{}, err
	}
	return value.Elem(), nil
}

func schemaFor(current reflect.Type) (message.JSONSchema, error) {
	for current.Kind() == reflect.Pointer {
		current = current.Elem()
	}
	switch current.Kind() {
	case reflect.Struct:
		properties := map[string]message.JSONSchema{}
		required := make([]string, 0, current.NumField())
		for idx := 0; idx < current.NumField(); idx++ {
			field := current.Field(idx)
			if !field.IsExported() {
				continue
			}
			name := field.Tag.Get("json")
			name = strings.Split(name, ",")[0]
			if name == "" {
				name = lowerCamel(field.Name)
			}
			if name == "-" {
				continue
			}
			child, err := schemaFor(field.Type)
			if err != nil {
				return message.JSONSchema{}, err
			}
			if description := field.Tag.Get("description"); description != "" {
				child.Description = description
			}
			properties[name] = child
			if !strings.Contains(field.Tag.Get("json"), "omitempty") {
				required = append(required, name)
			}
		}
		return message.JSONSchema{
			Type:                 "object",
			Properties:           properties,
			Required:             required,
			AdditionalProperties: false,
		}, nil
	case reflect.Slice, reflect.Array:
		items, err := schemaFor(current.Elem())
		if err != nil {
			return message.JSONSchema{}, err
		}
		return message.JSONSchema{
			Type:  "array",
			Items: &items,
		}, nil
	case reflect.Bool:
		return message.JSONSchema{Type: "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return message.JSONSchema{Type: "integer"}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return message.JSONSchema{Type: "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return message.JSONSchema{Type: "number"}, nil
	case reflect.Map:
		return message.JSONSchema{Type: "object", AdditionalProperties: true}, nil
	case reflect.String:
		return message.JSONSchema{Type: "string"}, nil
	default:
		return message.JSONSchema{}, fmt.Errorf("unsupported schema type: %s", current.Kind())
	}
}

func lowerCamel(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToLower(value[:1]) + value[1:]
}
