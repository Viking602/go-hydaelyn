package matcher

import (
	"encoding/json"
	"fmt"
	"math/big"
)

// schemaNode is the parsed form of the supported JSON Schema keyword subset.
// It mirrors agent.OutputPolicy's validator so a schema that the agent loop
// accepts is graded identically here.
type schemaNode struct {
	typ                  string
	properties           map[string]schemaNode
	required             []string
	items                *schemaNode
	enum                 []any
	additionalProperties *bool
}

func parseSchema(raw json.RawMessage) (schemaNode, error) {
	if len(raw) == 0 {
		return schemaNode{}, nil
	}
	return parseSchemaNode(raw, "$")
}

func parseSchemaNode(raw json.RawMessage, path string) (schemaNode, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return schemaNode{}, fmt.Errorf("%s: schema must be a JSON object: %w", path, err)
	}
	for keyword := range fields {
		if !supportedKeyword(keyword) {
			return schemaNode{}, fmt.Errorf("%s: unsupported schema keyword %q", path, keyword)
		}
	}
	node := schemaNode{}
	if err := parseSchemaType(fields, path, &node); err != nil {
		return schemaNode{}, err
	}
	if rawRequired, ok := fields["required"]; ok {
		if err := json.Unmarshal(rawRequired, &node.required); err != nil {
			return schemaNode{}, fmt.Errorf("%s.required: %w", path, err)
		}
	}
	if err := parseSchemaProperties(fields, path, &node); err != nil {
		return schemaNode{}, err
	}
	if rawItems, ok := fields["items"]; ok {
		child, err := parseSchemaNode(rawItems, path+".items")
		if err != nil {
			return schemaNode{}, err
		}
		node.items = &child
	}
	if err := parseSchemaEnum(fields, path, &node); err != nil {
		return schemaNode{}, err
	}
	if rawAdditional, ok := fields["additionalProperties"]; ok {
		var allowed bool
		if err := json.Unmarshal(rawAdditional, &allowed); err != nil {
			return schemaNode{}, fmt.Errorf("%s.additionalProperties: %w", path, err)
		}
		node.additionalProperties = &allowed
	}
	return node, nil
}

func parseSchemaType(fields map[string]json.RawMessage, path string, node *schemaNode) error {
	rawType, ok := fields["type"]
	if !ok {
		return nil
	}
	if err := json.Unmarshal(rawType, &node.typ); err != nil {
		return fmt.Errorf("%s.type: %w", path, err)
	}
	if !supportedType(node.typ) {
		return fmt.Errorf("%s: unsupported schema type %q", path, node.typ)
	}
	return nil
}

func parseSchemaProperties(fields map[string]json.RawMessage, path string, node *schemaNode) error {
	rawProps, ok := fields["properties"]
	if !ok {
		return nil
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(rawProps, &properties); err != nil {
		return fmt.Errorf("%s.properties: %w", path, err)
	}
	node.properties = make(map[string]schemaNode, len(properties))
	for name, propRaw := range properties {
		child, err := parseSchemaNode(propRaw, childPath(path, name))
		if err != nil {
			return err
		}
		node.properties[name] = child
	}
	return nil
}

func parseSchemaEnum(fields map[string]json.RawMessage, path string, node *schemaNode) error {
	rawEnum, ok := fields["enum"]
	if !ok {
		return nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(rawEnum, &values); err != nil {
		return fmt.Errorf("%s.enum: %w", path, err)
	}
	node.enum = make([]any, 0, len(values))
	for _, value := range values {
		decoded, err := decodeJSON(value)
		if err != nil {
			return fmt.Errorf("%s.enum: %w", path, err)
		}
		node.enum = append(node.enum, decoded)
	}
	return nil
}

func supportedKeyword(keyword string) bool {
	switch keyword {
	case "type", "properties", "required", "items", "enum", "additionalProperties", "description":
		return true
	default:
		return false
	}
}

func supportedType(schemaType string) bool {
	switch schemaType {
	case "", "object", "array", "string", "number", "integer", "boolean":
		return true
	default:
		return false
	}
}

func validateAgainstSchema(value any, schema schemaNode, path string) error {
	if schema.typ != "" {
		if err := validateType(value, schema.typ, path); err != nil {
			return err
		}
	}
	if len(schema.enum) > 0 {
		if !enumContains(schema.enum, value) {
			return fmt.Errorf("%s: value is not in enum", path)
		}
	}
	if err := validateObject(value, schema, path); err != nil {
		return err
	}
	return validateArray(value, schema, path)
}

func validateType(value any, schemaType string, path string) error {
	switch schemaType {
	case "object":
		if _, ok := value.(map[string]any); ok {
			return nil
		}
	case "array":
		if _, ok := value.([]any); ok {
			return nil
		}
	case "string":
		if _, ok := value.(string); ok {
			return nil
		}
	case "number":
		if number, ok := value.(json.Number); ok && validNumber(number) {
			return nil
		}
	case "integer":
		if number, ok := value.(json.Number); ok && integerNumber(number) {
			return nil
		}
	case "boolean":
		if _, ok := value.(bool); ok {
			return nil
		}
	}
	return fmt.Errorf("%s: expected %s", path, schemaType)
}

func validateObject(value any, schema schemaNode, path string) error {
	if !requiresObject(schema) {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: expected object", path)
	}
	for _, name := range schema.required {
		if _, ok := object[name]; !ok {
			return fmt.Errorf("%s: missing required property %q", path, name)
		}
	}
	for name, child := range schema.properties {
		propertyValue, ok := object[name]
		if !ok {
			continue
		}
		if err := validateAgainstSchema(propertyValue, child, childPath(path, name)); err != nil {
			return err
		}
	}
	if schema.additionalProperties != nil && !*schema.additionalProperties {
		for name := range object {
			if _, ok := schema.properties[name]; !ok {
				return fmt.Errorf("%s: additional property %q is not allowed", path, name)
			}
		}
	}
	return nil
}

func requiresObject(schema schemaNode) bool {
	return schema.typ == "object" || len(schema.properties) > 0 || len(schema.required) > 0 || schema.additionalProperties != nil
}

func validateArray(value any, schema schemaNode, path string) error {
	if schema.typ != "array" && schema.items == nil {
		return nil
	}
	array, ok := value.([]any)
	if !ok {
		return fmt.Errorf("%s: expected array", path)
	}
	if schema.items == nil {
		return nil
	}
	for index, item := range array {
		if err := validateAgainstSchema(item, *schema.items, fmt.Sprintf("%s[%d]", path, index)); err != nil {
			return err
		}
	}
	return nil
}

func enumContains(enum []any, value any) bool {
	for _, candidate := range enum {
		if equalJSON(value, candidate) {
			return true
		}
	}
	return false
}

func validNumber(number json.Number) bool {
	_, ok := new(big.Rat).SetString(number.String())
	return ok
}

func integerNumber(number json.Number) bool {
	rat, ok := new(big.Rat).SetString(number.String())
	return ok && rat.IsInt()
}
