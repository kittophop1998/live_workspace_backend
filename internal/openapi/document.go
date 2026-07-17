// Package openapi converts an OpenAPI 3.x document into the flat endpoint
// shape the workspace importer consumes (method, path, request-body fields,
// per-status response fields). It mirrors the frontend parser in
// frontend/src/lib/specImport.ts so a spec synced by the CLI and a spec
// imported in the browser produce the same workspace resources.
package openapi

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// document is an order-preserving JSON/YAML mapping. Property order in an
// OpenAPI document is meaningful to readers, so fields must come out in
// authored order — which plain Go maps discard. Nested mappings are *document,
// sequences are []any, scalars are string/int/float64/bool/nil.
type document struct {
	keys   []string
	values map[string]any
}

func newDocument() *document { return &document{values: map[string]any{}} }

func (d *document) get(key string) any {
	if d == nil {
		return nil
	}
	return d.values[key]
}

func (d *document) getDoc(key string) *document {
	value, _ := d.get(key).(*document)
	return value
}

func (d *document) getString(key string) string {
	value, _ := d.get(key).(string)
	return value
}

func (d *document) set(key string, value any) {
	if _, exists := d.values[key]; !exists {
		d.keys = append(d.keys, key)
	}
	d.values[key] = value
}

const maxDocumentDepth = 200

func parseDocument(content, format string) (*document, error) {
	var value any
	var err error
	if format == "json" {
		value, err = parseJSON(content)
	} else {
		value, err = parseYAML(content)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid OpenAPI %s", strings.ToUpper(format))
	}
	root, ok := value.(*document)
	if !ok {
		return nil, fmt.Errorf("OpenAPI document must be a %s object", strings.ToUpper(format))
	}
	return root, nil
}

func parseYAML(content string) (any, error) {
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(content), &node); err != nil {
		return nil, err
	}
	if node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
		return nil, fmt.Errorf("empty document")
	}
	return convertYAML(node.Content[0], 0)
}

func convertYAML(node *yaml.Node, depth int) (any, error) {
	if depth > maxDocumentDepth {
		return nil, fmt.Errorf("document is nested too deeply")
	}
	switch node.Kind {
	case yaml.AliasNode:
		return convertYAML(node.Alias, depth+1)
	case yaml.MappingNode:
		out := newDocument()
		for i := 0; i+1 < len(node.Content); i += 2 {
			var key string
			if err := node.Content[i].Decode(&key); err != nil {
				key = node.Content[i].Value
			}
			value, err := convertYAML(node.Content[i+1], depth+1)
			if err != nil {
				return nil, err
			}
			out.set(key, value)
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(node.Content))
		for _, item := range node.Content {
			value, err := convertYAML(item, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	default:
		var value any
		if err := node.Decode(&value); err != nil {
			return nil, err
		}
		return value, nil
	}
}

func parseJSON(content string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	value, err := convertJSON(decoder, 0)
	if err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, fmt.Errorf("trailing content after JSON document")
	}
	return value, nil
}

func convertJSON(decoder *json.Decoder, depth int) (any, error) {
	if depth > maxDocumentDepth {
		return nil, fmt.Errorf("document is nested too deeply")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	return convertJSONToken(decoder, token, depth)
}

func convertJSONToken(decoder *json.Decoder, token json.Token, depth int) (any, error) {
	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			out := newDocument()
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, _ := keyToken.(string)
				value, err := convertJSON(decoder, depth+1)
				if err != nil {
					return nil, err
				}
				out.set(key, value)
			}
			_, err := decoder.Token() // consume '}'
			return out, err
		case '[':
			out := make([]any, 0)
			for decoder.More() {
				value, err := convertJSON(decoder, depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, value)
			}
			_, err := decoder.Token() // consume ']'
			return out, err
		}
		return nil, fmt.Errorf("unexpected delimiter %v", t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i), nil
		}
		f, err := t.Float64()
		return f, err
	default:
		return token, nil
	}
}

// plain converts a parsed value back into ordinary Go JSON values
// (map[string]any / []any / scalars) so it can be stored as a field's sample.
func plain(value any) any {
	switch v := value.(type) {
	case *document:
		out := make(map[string]any, len(v.keys))
		for _, key := range v.keys {
			out[key] = plain(v.values[key])
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = plain(item)
		}
		return out
	default:
		return v
	}
}
