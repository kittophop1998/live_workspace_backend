// Package arazzo parses an Arazzo (OpenAPI Workflows Specification) document —
// JSON or YAML — into the domain's FlowDefinition model. It is pure (no I/O, no
// persistence) so it can be unit-tested against fixtures; the usecase layer owns
// saving/running the result. This is the backend counterpart of the frontend's
// src/lib/specImport.ts.
package arazzo

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"kingdom_manager/backend/internal/domain/entity"
)

// stepRefPattern matches "$steps.<stepId>" references used to infer step
// dependencies when the document doesn't declare them explicitly.
var stepRefPattern = regexp.MustCompile(`\$steps\.([A-Za-z0-9_\-]+)`)

// Parser implements port.FlowParser without holding mutable state.
type Parser struct{}

func (Parser) Parse(data []byte) ([]entity.FlowDefinition, error) {
	return Parse(data)
}

// Parse decodes an Arazzo document and returns one FlowDefinition per workflow.
// YAML is a superset of JSON, so a single YAML decode handles both formats and
// yields consistent map[string]any types.
func Parse(data []byte) ([]entity.FlowDefinition, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, fmt.Errorf("empty workflow file")
	}
	var root map[string]any
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("file is not valid JSON or YAML: %w", err)
	}
	workflows, ok := asSlice(root["workflows"])
	if !ok || len(workflows) == 0 {
		return nil, fmt.Errorf("no workflows found — expected an Arazzo document with a workflows[] array")
	}

	flows := make([]entity.FlowDefinition, 0, len(workflows))
	for _, raw := range workflows {
		wf, ok := asMap(raw)
		if !ok {
			continue
		}
		flows = append(flows, parseWorkflow(wf))
	}
	if len(flows) == 0 {
		return nil, fmt.Errorf("no valid workflows found in document")
	}
	return flows, nil
}

func parseWorkflow(wf map[string]any) entity.FlowDefinition {
	flow := entity.FlowDefinition{
		Name:        firstString(wf, "workflowId", "name"),
		Description: firstString(wf, "description", "summary"),
		Inputs:      parseInputs(wf["inputs"]),
	}
	if flow.Name == "" {
		flow.Name = "workflow"
	}

	stepsRaw, _ := asSlice(wf["steps"])
	stepIDs := make(map[string]bool)
	for _, raw := range stepsRaw {
		if step, ok := asMap(raw); ok {
			if id := firstString(step, "stepId", "id"); id != "" {
				stepIDs[id] = true
			}
		}
	}

	for i, raw := range stepsRaw {
		step, ok := asMap(raw)
		if !ok {
			continue
		}
		flow.Steps = append(flow.Steps, parseStep(step, i, stepIDs))
	}
	return flow
}

func parseStep(step map[string]any, order int, knownSteps map[string]bool) entity.FlowStep {
	out := entity.FlowStep{
		StepID:          firstString(step, "stepId", "id"),
		Description:     firstString(step, "description"),
		OperationID:     firstString(step, "operationId"),
		Order:           order,
		Parameters:      parseParameters(step["parameters"]),
		Outputs:         parseOutputs(step["outputs"]),
		SuccessCriteria: parseCriteria(step["successCriteria"]),
	}
	if out.StepID == "" {
		out.StepID = fmt.Sprintf("step%d", order+1)
	}

	// Method/path: explicit fields win; otherwise try to recover them from an
	// Arazzo operationPath runtime expression (…#/paths/~1pet/get).
	out.Method = strings.ToUpper(firstString(step, "method"))
	out.Path = firstString(step, "path")
	if out.Method == "" || out.Path == "" {
		if method, path, ok := parseOperationPath(firstString(step, "operationPath")); ok {
			if out.Method == "" {
				out.Method = method
			}
			if out.Path == "" {
				out.Path = path
			}
		}
	}

	if body, ok := asMap(step["requestBody"]); ok {
		// Arazzo wraps the body: { contentType, payload }.
		if payload, exists := body["payload"]; exists {
			out.RequestBody = payload
		} else {
			out.RequestBody = body
		}
	} else if body, exists := step["requestBody"]; exists {
		out.RequestBody = body
	}

	out.DependsOn = dependencies(step, out, knownSteps)
	return out
}

// dependencies collects step dependencies: an explicit dependsOn array if given,
// plus any "$steps.<id>" references found anywhere in the step's parameters,
// body, outputs, or criteria (Arazzo expresses ordering implicitly this way).
func dependencies(step map[string]any, parsed entity.FlowStep, knownSteps map[string]bool) []string {
	seen := map[string]bool{}
	deps := []string{}
	add := func(id string) {
		if id == "" || id == parsed.StepID || seen[id] || !knownSteps[id] {
			return
		}
		seen[id] = true
		deps = append(deps, id)
	}
	if explicit, ok := asSlice(step["dependsOn"]); ok {
		for _, value := range explicit {
			add(asString(value))
		}
	}
	for _, match := range stepRefPattern.FindAllStringSubmatch(scanText(step), -1) {
		add(match[1])
	}
	sort.Strings(deps)
	return deps
}

func parseInputs(raw any) []entity.FlowVariable {
	schema, ok := asMap(raw)
	if !ok {
		return nil
	}
	props, ok := asMap(schema["properties"])
	if !ok {
		return nil
	}
	out := make([]entity.FlowVariable, 0, len(props))
	for name, def := range props {
		variable := entity.FlowVariable{Name: name, In: "input"}
		if defMap, ok := asMap(def); ok {
			if example, exists := defMap["example"]; exists {
				variable.Value = example
			} else if def, exists := defMap["default"]; exists {
				variable.Value = def
			}
		}
		out = append(out, variable)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func parseParameters(raw any) []entity.StepParameter {
	list, ok := asSlice(raw)
	if !ok {
		return nil
	}
	out := make([]entity.StepParameter, 0, len(list))
	for _, item := range list {
		param, ok := asMap(item)
		if !ok {
			continue
		}
		out = append(out, entity.StepParameter{
			Name:  firstString(param, "name"),
			In:    firstString(param, "in"),
			Value: param["value"],
		})
	}
	return out
}

// parseOutputs accepts Arazzo's map form ({ name: expression }).
func parseOutputs(raw any) []entity.FlowOutput {
	outputs, ok := asMap(raw)
	if !ok {
		return nil
	}
	out := make([]entity.FlowOutput, 0, len(outputs))
	for name, expr := range outputs {
		out = append(out, entity.FlowOutput{Name: name, From: asString(expr)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func parseCriteria(raw any) []entity.Criterion {
	list, ok := asSlice(raw)
	if !ok {
		return nil
	}
	out := make([]entity.Criterion, 0, len(list))
	for _, item := range list {
		crit, ok := asMap(item)
		if !ok {
			// A bare string is treated as a condition.
			if s := asString(item); s != "" {
				out = append(out, entity.Criterion{Condition: s})
			}
			continue
		}
		criterion := entity.Criterion{
			Condition: firstString(crit, "condition"),
			Context:   firstString(crit, "context"),
		}
		// `type` may be a string ("simple"|"regex"|"jsonpath") or an object.
		if t := firstString(crit, "type"); t != "" {
			criterion.Type = t
		} else if typeObj, ok := asMap(crit["type"]); ok {
			criterion.Type = firstString(typeObj, "type")
		}
		out = append(out, criterion)
	}
	return out
}

// parseOperationPath extracts method + path from a runtime expression such as
// "{$sourceDescriptions.petstore.url}#/paths/~1pet~1{petId}/get".
func parseOperationPath(expr string) (method, path string, ok bool) {
	hash := strings.Index(expr, "#")
	if hash < 0 {
		return "", "", false
	}
	pointer := expr[hash+1:]
	segments := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	// Expect: paths / <encoded-path> / <method>
	var pathsIdx = -1
	for i, seg := range segments {
		if seg == "paths" {
			pathsIdx = i
			break
		}
	}
	if pathsIdx < 0 || pathsIdx+2 >= len(segments) {
		return "", "", false
	}
	rawPath := decodePointer(segments[pathsIdx+1])
	method = strings.ToUpper(segments[pathsIdx+2])
	if rawPath == "" || method == "" {
		return "", "", false
	}
	return method, rawPath, true
}

func decodePointer(segment string) string {
	segment = strings.ReplaceAll(segment, "~1", "/")
	return strings.ReplaceAll(segment, "~0", "~")
}
