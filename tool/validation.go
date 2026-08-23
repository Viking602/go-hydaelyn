package tool

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	maxArgumentBytes = 1 << 20
	maxArgumentDepth = 128
)

var (
	ErrInvalidArguments     = errors.New("invalid tool arguments")
	ErrDuplicateArgumentKey = errors.New("duplicate tool argument key")
	ErrInvalidToolSchema    = errors.New("invalid tool schema")
)

type argumentValidation struct {
	schema *jsonschema.Schema
	err    error
}

func compileArgumentValidation(definition Definition) argumentValidation {
	raw, err := json.Marshal(definition.InputSchema)
	if err != nil {
		return argumentValidation{err: fmt.Errorf("%w for %s: %v", ErrInvalidToolSchema, definition.Name, err)}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return argumentValidation{err: fmt.Errorf("%w for %s: %v", ErrInvalidToolSchema, definition.Name, err)}
	}
	digest := sha256.Sum256(raw)
	resource := "https://venat.local/tool-schema/" + hex.EncodeToString(digest[:]) + ".json"
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource(resource, document); err != nil {
		return argumentValidation{err: fmt.Errorf("%w for %s: %v", ErrInvalidToolSchema, definition.Name, err)}
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		return argumentValidation{err: fmt.Errorf("%w for %s: %v", ErrInvalidToolSchema, definition.Name, err)}
	}
	return argumentValidation{schema: compiled}
}

func (validation argumentValidation) validate(arguments json.RawMessage) error {
	if validation.err != nil {
		return validation.err
	}
	if len(bytes.TrimSpace(arguments)) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	if len(arguments) > maxArgumentBytes {
		return fmt.Errorf("%w: payload exceeds %d bytes", ErrInvalidArguments, maxArgumentBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.UseNumber()
	value, err := decodeArgumentValue(decoder, 0)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidArguments, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return fmt.Errorf("%w: trailing data: %v", ErrInvalidArguments, err)
	}
	if err := validation.schema.Validate(value); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidArguments, err)
	}
	return nil
}

func decodeArgumentValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > maxArgumentDepth {
		return nil, fmt.Errorf("JSON nesting exceeds %d levels", maxArgumentDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string")
			}
			if _, duplicate := object[key]; duplicate {
				return nil, fmt.Errorf("%w %q", ErrDuplicateArgumentKey, key)
			}
			value, err := decodeArgumentValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
			return nil, fmt.Errorf("unterminated object")
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := decodeArgumentValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
			return nil, fmt.Errorf("unterminated array")
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %q", delimiter)
	}
}
