package arazzo

import (
	"encoding/json"
	"fmt"
)

// Loose accessors over decoded JSON/YAML (map[string]any) — the document is
// untrusted user input, so every lookup tolerates missing/mistyped fields.

func asMap(value any) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	return m, ok
}

func asSlice(value any) ([]any, bool) {
	s, ok := value.([]any)
	return s, ok
}

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// firstString returns the first non-empty string value among the given keys.
func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := m[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// scanText renders a value to a JSON string so regexes can find embedded
// runtime-expression references (e.g. "$steps.loginStep.outputs.token").
func scanText(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}
