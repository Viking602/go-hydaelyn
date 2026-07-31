package coding

import "github.com/Viking602/venat/message"

// property is one named field in an object schema.
type property struct {
	name   string
	schema message.JSONSchema
}

// stringArraySchema builds a schema for an array of strings.
func stringArraySchema(description string) message.JSONSchema {
	items := message.JSONSchema{Type: "string"}
	return message.JSONSchema{Type: "array", Description: description, Items: &items}
}

// objectSchema builds a JSON-schema object from ordered properties, marking
// the named fields required. AdditionalProperties is disabled so the model
// cannot smuggle extra arguments past the typed input.
func objectSchema(required []string, props ...property) message.JSONSchema {
	properties := make(map[string]message.JSONSchema, len(props))
	for _, p := range props {
		properties[p.name] = p.schema
	}
	additional := false
	return message.JSONSchema{
		Type:                 "object",
		Properties:           properties,
		Required:             required,
		AdditionalProperties: &additional,
	}
}
