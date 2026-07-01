package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

// runContext accumulates values available to runtime expressions during a run:
// the flow inputs and each completed step's extracted outputs.
type runContext struct {
	inputs map[string]any
	steps  map[string]map[string]any
}

var exprToken = regexp.MustCompile(`\$[A-Za-z0-9_.#/\-\[\]]+`)

// runStep resolves, sends, and validates a single step.
func (s *FlowService) runStep(ctx context.Context, baseURL string, step entity.FlowStep, operations map[string]operationRef, rc *runContext) entity.StepResult {
	result := entity.StepResult{StepID: step.StepID, Outputs: map[string]any{}}

	method, path := step.Method, step.Path
	if (method == "" || path == "") && step.OperationID != "" {
		if op, ok := operations[strings.ToLower(step.OperationID)]; ok {
			if method == "" {
				method = op.method
			}
			if path == "" {
				path = op.path
			}
		}
	}
	if method == "" {
		method = "GET"
	}
	if path == "" {
		result.Error = fmt.Sprintf("could not resolve an HTTP path for step %q (no method/path and operationId %q not found in the workspace API spec)", step.StepID, step.OperationID)
		return result
	}

	// Split parameters by location and resolve their values.
	query := url.Values{}
	headers := map[string]string{}
	for _, param := range step.Parameters {
		value := stringifyValue(rc.resolve(param.Value))
		switch strings.ToLower(param.In) {
		case "path":
			path = strings.ReplaceAll(path, "{"+param.Name+"}", url.PathEscape(value))
		case "query":
			query.Add(param.Name, value)
		case "header":
			headers[param.Name] = value
		}
	}

	var body []byte
	if step.RequestBody != nil {
		resolved := rc.resolveDeep(step.RequestBody)
		if encoded, err := json.Marshal(resolved); err == nil {
			body = encoded
			result.RequestBody = string(encoded)
			if _, exists := headers["Content-Type"]; !exists {
				headers["Content-Type"] = "application/json"
			}
		}
	}

	target := baseURL + path
	if encoded := query.Encode(); encoded != "" {
		if strings.Contains(target, "?") {
			target += "&" + encoded
		} else {
			target += "?" + encoded
		}
	}

	result.Method, result.URL = method, target
	resp, err := s.exec.Exec(ctx, port.HTTPRequest{Method: method, URL: target, Headers: headers, Body: body})
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Status = resp.Status
	result.DurationMs = resp.DurationMs
	result.Response = resp.Body

	// Parse the response body once (used by criteria + output extraction).
	var parsedBody any
	_ = json.Unmarshal([]byte(resp.Body), &parsedBody)

	result.Passed, result.Failures = evaluateCriteria(step.SuccessCriteria, resp, parsedBody)

	// Extract declared outputs into the shared context for later steps.
	for _, output := range step.Outputs {
		value := extractExpr(output.From, resp, parsedBody, rc)
		result.Outputs[output.Name] = value
	}
	if len(step.Outputs) > 0 {
		rc.steps[step.StepID] = result.Outputs
	}
	return result
}

// evaluateCriteria applies each success criterion. With no criteria, a 2xx
// status passes. Unsupported conditions fail explicitly instead of silently
// producing a false-positive run.
func evaluateCriteria(criteria []entity.Criterion, resp port.HTTPResponse, parsedBody any) (bool, []string) {
	if len(criteria) == 0 {
		if resp.Status >= 200 && resp.Status < 300 {
			return true, nil
		}
		return false, []string{fmt.Sprintf("expected 2xx status, got %d", resp.Status)}
	}
	failures := []string{}
	for _, criterion := range criteria {
		ok, detail := evalCondition(criterion, resp, parsedBody)
		if !ok {
			failures = append(failures, detail)
		}
	}
	return len(failures) == 0, failures
}

var comparison = regexp.MustCompile(`^(.*?)(==|!=|>=|<=|>|<)(.*)$`)

// evalCondition supports the common Arazzo condition shapes:
//   - "$statusCode == 200"
//   - "$response.body#/status == \"ok\""
//   - "$response.body#/items/0/id != null"
//
// A regex-type criterion tests the (optionally context-scoped) value against the
// condition as a pattern. Anything we can't parse is treated as satisfied.
func evalCondition(criterion entity.Criterion, resp port.HTTPResponse, parsedBody any) (bool, string) {
	condition := strings.TrimSpace(criterion.Condition)
	if condition == "" {
		return true, ""
	}
	if strings.EqualFold(criterion.Type, "regex") {
		value := stringifyValue(extractExpr(criterion.Context, resp, parsedBody, nil))
		re, err := regexp.Compile(condition)
		if err != nil {
			return false, fmt.Sprintf("invalid regex %q", condition)
		}
		if re.MatchString(value) {
			return true, ""
		}
		return false, fmt.Sprintf("regex %q did not match %q", condition, value)
	}

	match := comparison.FindStringSubmatch(condition)
	if match == nil {
		return false, fmt.Sprintf("unsupported criterion: %s", condition)
	}
	left := strings.TrimSpace(match[1])
	operator := match[2]
	right := strings.TrimSpace(match[3])

	actual := extractExpr(left, resp, parsedBody, nil)
	expected := parseLiteral(right)
	ok := compareValues(actual, expected, operator)
	if ok {
		return true, ""
	}
	return false, fmt.Sprintf("criterion failed: %s (actual=%v)", condition, actual)
}

func compareValues(actual, expected any, operator string) bool {
	// Numeric comparison when both sides look like numbers.
	an, aok := toFloat(actual)
	en, eok := toFloat(expected)
	if aok && eok {
		switch operator {
		case "==":
			return an == en
		case "!=":
			return an != en
		case ">":
			return an > en
		case ">=":
			return an >= en
		case "<":
			return an < en
		case "<=":
			return an <= en
		}
	}
	as, es := stringifyValue(actual), stringifyValue(expected)
	switch operator {
	case "==":
		return as == es
	case "!=":
		return as != es
	default:
		return false
	}
}

// extractExpr evaluates a runtime expression against the response + context.
// Supports: $statusCode, $response.body, $response.body#/ptr, $response.header.X,
// $inputs.name, $steps.stepId.outputs.name. Non-expression text is returned as-is.
func extractExpr(expr string, resp port.HTTPResponse, parsedBody any, rc *runContext) any {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil
	}
	if !strings.HasPrefix(expr, "$") {
		return parseLiteral(expr)
	}
	switch {
	case expr == "$statusCode":
		return resp.Status
	case expr == "$response.body":
		if parsedBody != nil {
			return parsedBody
		}
		return resp.Body
	case strings.HasPrefix(expr, "$response.body#"):
		return jsonPointer(parsedBody, strings.TrimPrefix(expr, "$response.body#"))
	case strings.HasPrefix(expr, "$response.header."):
		name := strings.TrimPrefix(expr, "$response.header.")
		if values, ok := resp.Headers[canonicalHeader(name)]; ok && len(values) > 0 {
			return values[0]
		}
		return nil
	case strings.HasPrefix(expr, "$inputs."):
		if rc != nil {
			return rc.inputs[strings.TrimPrefix(expr, "$inputs.")]
		}
	case strings.HasPrefix(expr, "$steps."):
		if rc != nil {
			return rc.resolveStepExpr(strings.TrimPrefix(expr, "$steps."))
		}
	}
	return nil
}

// resolve evaluates a single value: expression strings (with embedded $tokens)
// are substituted; everything else passes through unchanged.
func (rc *runContext) resolve(value any) any {
	s, ok := value.(string)
	if !ok {
		return value
	}
	trimmed := strings.TrimSpace(s)
	// Whole-string expression → return the raw resolved value (keeps its type).
	if strings.HasPrefix(trimmed, "$") && exprToken.FindString(trimmed) == trimmed {
		if v, ok := rc.lookup(trimmed); ok {
			return v
		}
		return nil
	}
	// Otherwise interpolate any embedded $tokens into the string.
	return exprToken.ReplaceAllStringFunc(s, func(token string) string {
		if v, ok := rc.lookup(token); ok {
			return stringifyValue(v)
		}
		return token
	})
}

// resolveDeep walks maps/slices resolving string leaves.
func (rc *runContext) resolveDeep(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = rc.resolveDeep(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = rc.resolveDeep(item)
		}
		return out
	default:
		return rc.resolve(value)
	}
}

func (rc *runContext) lookup(expr string) (any, bool) {
	switch {
	case strings.HasPrefix(expr, "$inputs."):
		v, ok := rc.inputs[strings.TrimPrefix(expr, "$inputs.")]
		return v, ok
	case strings.HasPrefix(expr, "$steps."):
		return rc.resolveStepExpr(strings.TrimPrefix(expr, "$steps.")), true
	}
	return nil, false
}

// resolveStepExpr reads "stepId.outputs.name" from accumulated step outputs.
func (rc *runContext) resolveStepExpr(rest string) any {
	parts := strings.SplitN(rest, ".", 3)
	if len(parts) < 3 || parts[1] != "outputs" {
		return nil
	}
	outputs, ok := rc.steps[parts[0]]
	if !ok {
		return nil
	}
	return outputs[parts[2]]
}

// jsonPointer resolves an RFC-6901 pointer ("/a/0/b") against decoded JSON.
func jsonPointer(root any, pointer string) any {
	pointer = strings.TrimSpace(pointer)
	if pointer == "" || pointer == "/" {
		return root
	}
	current := root
	for _, token := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch node := current.(type) {
		case map[string]any:
			current = node[token]
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(node) {
				return nil
			}
			current = node[index]
		default:
			return nil
		}
	}
	return current
}

func parseLiteral(text string) any {
	text = strings.TrimSpace(text)
	switch text {
	case "true":
		return true
	case "false":
		return false
	case "null", "":
		return nil
	}
	if strings.HasPrefix(text, "\"") && strings.HasSuffix(text, "\"") && len(text) >= 2 {
		return text[1 : len(text)-1]
	}
	if strings.HasPrefix(text, "'") && strings.HasSuffix(text, "'") && len(text) >= 2 {
		return text[1 : len(text)-1]
	}
	if number, err := strconv.ParseFloat(text, 64); err == nil {
		return number
	}
	return text
}

func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		f, err := typed.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(typed, 64)
		return f, err == nil
	}
	return 0, false
}

func stringifyValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		if encoded, err := json.Marshal(typed); err == nil {
			return string(encoded)
		}
		return fmt.Sprintf("%v", value)
	}
}

// canonicalHeader mimics textproto canonicalization for header lookups.
func canonicalHeader(name string) string {
	parts := strings.Split(name, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, "-")
}
