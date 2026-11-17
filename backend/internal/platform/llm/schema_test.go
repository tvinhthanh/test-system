package llm

import (
	"encoding/json"
	"testing"
)

func TestValidate(t *testing.T) {
	step := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"do", "value"},
		"properties": map[string]any{
			"do":    map[string]any{"type": "string", "enum": []string{"click", "fill"}},
			"value": map[string]any{"type": "string"},
		},
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"steps", "count"},
		"properties": map[string]any{
			"steps": map[string]any{"type": "array", "items": step},
			"count": map[string]any{"type": "integer"},
			"note":  map[string]any{"type": []any{"string", "null"}},
		},
	}
	for answer, ok := range map[string]bool{
		`{"steps":[{"do":"click","value":""}],"count":1}`:           true,
		`{"steps":[],"count":0,"note":null}`:                        true,
		`{"steps":[],"count":0,"note":"x"}`:                         true,
		`{"steps":[{"do":"hover","value":""}],"count":1}`:           false, // not in the enum
		`{"steps":[{"do":"click"}],"count":1}`:                      false, // required
		`{"steps":[{"do":"click","value":"","extra":1}],"count":1}`: false, // additionalProperties
		`{"steps":[],"count":1.5}`:                                  false, // integer
		`{"steps":{},"count":1}`:                                    false, // array
		`{"steps":[],"count":1,"note":3}`:                           false, // string or null
		`[]`:                                                        false,
		`{"steps":[],"count":1} trailing`:                           true, // one value is read; json.Valid has already judged the text
	} {
		if err := Validate(schema, json.RawMessage(answer)); (err == nil) != ok {
			t.Errorf("%s: %v", answer, err)
		}
	}
	if Validate(nil, json.RawMessage(`"anything"`)) != nil {
		t.Error("no schema, no check")
	}
}
