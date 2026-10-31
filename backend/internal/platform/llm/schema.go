package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Validate checks a JSON answer against the part of JSON Schema the prompts
// here use — type, enum, properties, required, additionalProperties: false,
// items. Only some providers hold an answer to a schema; JSON mode promises
// JSON and nothing more, so every answer is checked before a caller sees it
// (§11.6 API10). Other keywords are not checked.
func Validate(schema map[string]any, raw json.RawMessage) error {
	if schema == nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return err
	}
	return check(schema, v, "$")
}

func check(s map[string]any, v any, at string) error {
	if t, ok := s["type"]; ok && !typeIs(t, v) {
		return fmt.Errorf("%s: want %v", at, t)
	}
	if e, ok := s["enum"]; ok && !inEnum(e, v) {
		return fmt.Errorf("%s: %v is not one of %v", at, v, e)
	}
	switch x := v.(type) {
	case map[string]any:
		for _, name := range words(s["required"]) {
			if _, ok := x[name]; !ok {
				return fmt.Errorf("%s: %q is missing", at, name)
			}
		}
		props, _ := s["properties"].(map[string]any)
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sub, ok := props[k].(map[string]any)
			if !ok {
				if s["additionalProperties"] == false {
					return fmt.Errorf("%s: %q is not expected", at, k)
				}
				continue
			}
			if err := check(sub, x[k], at+"."+k); err != nil {
				return err
			}
		}
	case []any:
		if items, ok := s["items"].(map[string]any); ok {
			for i, item := range x {
				if err := check(items, item, fmt.Sprintf("%s[%d]", at, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// typeIs: t is a type name or a list of them.
func typeIs(t, v any) bool {
	for _, name := range words(t) {
		switch x := v.(type) {
		case map[string]any:
			if name == "object" {
				return true
			}
		case []any:
			if name == "array" {
				return true
			}
		case string:
			if name == "string" {
				return true
			}
		case bool:
			if name == "boolean" {
				return true
			}
		case nil:
			if name == "null" {
				return true
			}
		case json.Number:
			if name == "number" || name == "integer" && !strings.ContainsAny(x.String(), ".eE") {
				return true
			}
		}
	}
	return false
}

func inEnum(e, v any) bool {
	switch list := e.(type) {
	case []string:
		s, ok := v.(string)
		for _, w := range list {
			if ok && w == s {
				return true
			}
		}
		return false
	case []any:
		for _, w := range list {
			if fmt.Sprint(w) == fmt.Sprint(v) {
				return true
			}
		}
		return false
	}
	return true
}

// words reads a schema keyword that holds one string or a list of them.
func words(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, w := range x {
			if s, ok := w.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
