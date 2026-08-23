package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

const (
	maxExtraBodyDepth = 64
	maxExtraBodyBytes = 1 << 20
)

// ValidateExtraBody accepts JSON-shaped provider wire fields only. Host
// callbacks, services, pointers, channels, and arbitrary structs must use the
// typed Request fields instead.
func ValidateExtraBody(body map[string]any) error {
	if body == nil {
		return nil
	}
	if err := validateExtraBodyValue(reflect.ValueOf(body), 0, "extraBody"); err != nil {
		return err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("provider extra body is not valid JSON: %w", err)
	}
	if len(encoded) > maxExtraBodyBytes {
		return fmt.Errorf("provider extra body exceeds %d bytes", maxExtraBodyBytes)
	}
	return nil
}

func validateExtraBodyValue(value reflect.Value, depth int, path string) error {
	if depth > maxExtraBodyDepth {
		return fmt.Errorf("provider extra body %s exceeds %d levels", path, maxExtraBodyDepth)
	}
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return validateExtraBodyValue(value.Elem(), depth+1, path)
	}
	if value.Type() == reflect.TypeFor[json.RawMessage]() {
		return validateRawExtraBody(value.Bytes(), depth, path)
	}
	switch value.Kind() {
	case reflect.Bool, reflect.String:
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return nil
	case reflect.Float32, reflect.Float64:
		if number := value.Float(); math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("provider extra body %s contains a non-finite number", path)
		}
		return nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("provider extra body %s has non-string object keys", path)
		}
		iterator := value.MapRange()
		for iterator.Next() {
			key := iterator.Key().String()
			if err := validateExtraBodyValue(iterator.Value(), depth+1, path+"."+key); err != nil {
				return err
			}
		}
		return nil
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			if err := validateExtraBodyValue(value.Index(index), depth+1, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("provider extra body %s contains unsupported %s; use a typed Request field", path, value.Kind())
	}
}

func validateRawExtraBody(raw []byte, depth int, path string) error {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > maxExtraBodyBytes {
		return fmt.Errorf("provider extra body %s exceeds %d bytes", path, maxExtraBodyBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("provider extra body %s contains invalid raw JSON: %w", path, err)
	}
	return validateExtraBodyValue(reflect.ValueOf(decoded), depth+1, path)
}
