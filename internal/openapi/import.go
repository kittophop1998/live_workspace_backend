package openapi

import (
	"strconv"
	"strings"
)

// Field is a flat request/response body field, matching the workspace
// importer's input: nested objects and arrays-of-objects collapse to a "json"
// field carrying a representative sample value.
type Field struct {
	Key         string
	Type        string
	Required    bool
	Description *string
	Value       any
}

// Response is one per-status response schema ("default" maps to status 0).
type Response struct {
	Status      int
	Description *string
	Fields      []Field
}

// Endpoint is one operation from the document's paths.
type Endpoint struct {
	Name      string
	Method    string
	Path      string
	Fields    []Field
	Responses []Response
}

var methods = []string{"get", "post", "put", "patch", "delete"}

// Endpoints parses an OpenAPI 3.x document (format "yaml" or "json") into the
// operations under `paths`, in authored order.
func Endpoints(content, format string) ([]Endpoint, error) {
	root, err := parseDocument(content, format)
	if err != nil {
		return nil, err
	}
	paths := root.getDoc("paths")
	out := []Endpoint{}
	if paths == nil {
		return out, nil
	}
	for _, path := range paths.keys {
		item := paths.getDoc(path)
		if item == nil {
			continue
		}
		for _, m := range methods {
			op := item.getDoc(m)
			if op == nil {
				continue
			}
			method := strings.ToUpper(m)
			name := method + " " + path
			if id := strings.TrimSpace(op.getString("operationId")); id != "" {
				name = id
			}
			out = append(out, Endpoint{
				Name:      name,
				Method:    method,
				Path:      path,
				Fields:    requestFields(op, root),
				Responses: responses(op, root),
			})
		}
	}
	return out, nil
}

// jsonSchema extracts { content: { "application/json": { schema } } } from a
// request body or response, tolerating any *json media type and falling back
// to the first media type.
func jsonSchema(carrier *document) any {
	if carrier == nil {
		return nil
	}
	content := carrier.getDoc("content")
	if content == nil {
		return nil
	}
	mediaType := ""
	for _, key := range content.keys {
		if strings.Contains(key, "json") {
			mediaType = key
			break
		}
	}
	if mediaType == "" && len(content.keys) > 0 {
		mediaType = content.keys[0]
	}
	media := content.getDoc(mediaType)
	if media == nil {
		return nil
	}
	return media.get("schema")
}

func requestFields(op, root *document) []Field {
	// requestBody may itself be a $ref to #/components/requestBodies/* — deref first.
	body := deref(op.get("requestBody"), root, nil)
	if body == nil {
		body, _ = op.get("requestBody").(*document)
	}
	schema := jsonSchema(body)
	if schema == nil {
		return []Field{}
	}
	return schemaToFields(schema, root, nil)
}

func responses(op, root *document) []Response {
	responsesDoc := op.getDoc("responses")
	if responsesDoc == nil {
		return []Response{}
	}
	out := []Response{}
	for _, code := range responsesDoc.keys {
		status, ok := responseStatus(code)
		if !ok {
			continue
		}
		// A response entry may be a $ref to #/components/responses/* — deref first.
		response := deref(responsesDoc.get(code), root, nil)
		if response == nil {
			response = responsesDoc.getDoc(code)
		}
		fields := []Field{}
		if schema := jsonSchema(response); schema != nil {
			fields = schemaToFields(schema, root, nil)
		}
		var description *string
		if response != nil {
			if text := response.getString("description"); text != "" {
				description = &text
			}
		}
		out = append(out, Response{Status: status, Description: description, Fields: fields})
	}
	// Stable order by status, keeping "default" (0) first.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Status > out[j].Status; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// responseStatus accepts "default" (0) and concrete codes; range keys such as
// "2XX" and other non-numeric keys are skipped so one odd entry cannot make
// the whole workspace import fail validation.
func responseStatus(code string) (int, bool) {
	if code == "default" {
		return 0, true
	}
	status, err := strconv.Atoi(code)
	if err != nil || status < 100 || status > 599 {
		return 0, false
	}
	return status, true
}

func resolveRef(ref string, root *document) any {
	// "#/components/schemas/User" → root.components.schemas.User
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	var current any = root
	for _, segment := range strings.Split(ref[2:], "/") {
		doc, ok := current.(*document)
		if !ok {
			return nil
		}
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		current = doc.get(segment)
	}
	return current
}

func deref(schema any, root *document, seen map[string]bool) *document {
	doc, ok := schema.(*document)
	if !ok {
		return nil
	}
	if ref, ok := doc.get("$ref").(string); ok {
		if seen[ref] {
			return nil // cycle guard
		}
		next := clone(seen)
		next[ref] = true
		return deref(resolveRef(ref, root), root, next)
	}
	return doc
}

func clone(seen map[string]bool) map[string]bool {
	out := make(map[string]bool, len(seen)+1)
	for key := range seen {
		out[key] = true
	}
	return out
}

// schemaToFields turns a top-level object schema into one field per property.
// Nested objects/arrays-of-objects collapse to a `json` field carrying a
// sample shape. A non-object schema becomes a single representative "data"
// field. Only `json` fields may carry a value — the workspace importer rejects
// a value on any other type.
func schemaToFields(schemaRaw any, root *document, seen map[string]bool) []Field {
	schema := deref(schemaRaw, root, clone(seen))
	if schema == nil {
		return []Field{}
	}
	merged := mergeAllOf(schema, root, seen)
	props := merged.getDoc("properties")
	if props == nil {
		fieldType := openAPIType(merged, root, seen)
		field := Field{Key: "data", Type: fieldType, Required: true}
		if fieldType == "json" {
			field.Value = sampleFromSchema(merged, root, clone(seen), 0)
		}
		return []Field{field}
	}

	required := map[string]bool{}
	if values, ok := merged.get("required").([]any); ok {
		for _, value := range values {
			if key, ok := value.(string); ok {
				required[key] = true
			}
		}
	}
	out := make([]Field, 0, len(props.keys))
	for _, key := range props.keys {
		propRaw := props.get(key)
		prop := deref(propRaw, root, clone(seen))
		if prop == nil {
			prop, _ = propRaw.(*document)
		}
		fieldType := openAPIType(propRaw, root, seen)
		field := Field{Key: key, Type: fieldType, Required: required[key]}
		if prop != nil {
			if text := prop.getString("description"); text != "" {
				field.Description = &text
			}
		}
		if fieldType == "json" {
			field.Value = sampleFromSchema(propRaw, root, clone(seen), 0)
		}
		out = append(out, field)
	}
	return out
}

func mergeAllOf(schema *document, root *document, seen map[string]bool) *document {
	parts, ok := schema.get("allOf").([]any)
	if !ok {
		return schema
	}
	out := newDocument()
	for _, key := range schema.keys {
		out.set(key, schema.values[key])
	}
	properties := newDocument()
	if existing := schema.getDoc("properties"); existing != nil {
		for _, key := range existing.keys {
			properties.set(key, existing.values[key])
		}
	}
	required := []any{}
	if values, ok := schema.get("required").([]any); ok {
		required = append(required, values...)
	}
	for _, partRaw := range parts {
		part := deref(partRaw, root, clone(seen))
		if part == nil {
			continue
		}
		sub := mergeAllOf(part, root, seen)
		if subProps := sub.getDoc("properties"); subProps != nil {
			for _, key := range subProps.keys {
				properties.set(key, subProps.values[key])
			}
		}
		if values, ok := sub.get("required").([]any); ok {
			required = append(required, values...)
		}
	}
	out.set("properties", properties)
	out.set("required", required)
	return out
}

func schemaType(schema *document) string {
	switch value := schema.get("type").(type) {
	case string:
		return value
	case []any: // OpenAPI 3.1 type arrays, e.g. ["string", "null"]
		for _, item := range value {
			if text, ok := item.(string); ok && text != "null" {
				return text
			}
		}
	}
	return ""
}

func openAPIType(schemaRaw any, root *document, seen map[string]bool) string {
	schema := deref(schemaRaw, root, clone(seen))
	if schema == nil {
		return "json" // unresolved $ref → treat as nested object
	}
	if _, ok := schema.get("enum").([]any); ok {
		return "enum"
	}
	switch schemaType(schema) {
	case "string":
		switch schema.getString("format") {
		case "uuid", "guid":
			return "uuid"
		case "date-time", "date":
			return "timestamp"
		}
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		item := deref(schema.get("items"), root, clone(seen))
		if item != nil {
			switch schemaType(item) {
			case "string":
				return "string[]"
			case "integer", "number":
				return "number[]"
			}
		}
		return "json"
	default:
		if schema.getDoc("properties") != nil || schema.get("$ref") != nil {
			return "json"
		}
		return "string"
	}
}

// sampleFromSchema builds a representative JSON value for a `json` field
// (depth-capped, cycle-safe).
func sampleFromSchema(schemaRaw any, root *document, seen map[string]bool, depth int) any {
	schema := deref(schemaRaw, root, seen)
	if schema == nil || depth > 4 {
		return map[string]any{}
	}
	if value := schema.get("example"); value != nil {
		return plain(value)
	}
	if value := schema.get("default"); value != nil {
		return plain(value)
	}
	if values, ok := schema.get("enum").([]any); ok && len(values) > 0 {
		return plain(values[0])
	}

	valueType := schemaType(schema)
	if valueType == "array" {
		return []any{sampleFromSchema(schema.get("items"), root, clone(seen), depth+1)}
	}
	if valueType == "object" || schema.getDoc("properties") != nil {
		merged := mergeAllOf(schema, root, seen)
		props := merged.getDoc("properties")
		out := map[string]any{}
		if props != nil {
			for _, key := range props.keys {
				out[key] = sampleFromSchema(props.get(key), root, clone(seen), depth+1)
			}
		}
		return out
	}
	switch valueType {
	case "string":
		if schema.getString("format") == "uuid" {
			return "00000000-0000-4000-8000-000000000000"
		}
		return "string"
	case "integer", "number":
		return 0
	case "boolean":
		return false
	default:
		return nil
	}
}
