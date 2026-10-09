// Package jsonvalue parses bounded, duplicate-free JSON without rounding numbers.
package jsonvalue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Parse reads one JSON value. Keys must be unique within each object and nesting
// is limited to 100 levels. Callers enforce their own byte limits before parsing.
func Parse(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := readValue(decoder, "$", 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON value")
	}
	return value, nil
}

func readValue(decoder *json.Decoder, path string, depth int) (any, error) {
	if depth > 100 {
		return nil, fmt.Errorf("%s: nesting exceeds 100 levels", path)
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("%s: malformed JSON", path)
	}
	switch token {
	case json.Delim('{'):
		object := map[string]any{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, fmt.Errorf("%s: invalid object key", path)
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("%s: object key must be a string", path)
			}
			if _, exists := object[name]; exists {
				return nil, fmt.Errorf("%s.%s: duplicate key", path, name)
			}
			value, err := readValue(decoder, path+"."+name, depth+1)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
			return nil, fmt.Errorf("%s: unterminated object", path)
		}
		return object, nil
	case json.Delim('['):
		array := []any{}
		for decoder.More() {
			value, err := readValue(decoder, fmt.Sprintf("%s[%d]", path, len(array)), depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
			return nil, fmt.Errorf("%s: unterminated array", path)
		}
		return array, nil
	default:
		if _, delimiter := token.(json.Delim); delimiter {
			return nil, fmt.Errorf("%s: unexpected delimiter", path)
		}
		return token, nil
	}
}
