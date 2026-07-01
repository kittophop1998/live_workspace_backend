package arazzo

import "testing"

const sampleYAML = `
arazzo: 1.0.0
info:
  title: Test
  version: 1.0.0
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: loginAndFetch
    summary: Login then fetch
    description: Logs in and fetches the profile
    inputs:
      type: object
      properties:
        username:
          type: string
          example: alice
        password:
          type: string
    steps:
      - stepId: login
        operationId: loginUser
        parameters:
          - name: username
            in: query
            value: $inputs.username
        requestBody:
          contentType: application/json
          payload:
            password: $inputs.password
        successCriteria:
          - condition: $statusCode == 200
        outputs:
          token: $response.body#/token
      - stepId: profile
        operationPath: "{$sourceDescriptions.api.url}#/paths/~1users~1{id}/get"
        parameters:
          - name: Authorization
            in: header
            value: $steps.login.outputs.token
        successCriteria:
          - condition: $statusCode == 200
`

func TestParseYAML(t *testing.T) {
	flows, err := Parse([]byte(sampleYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("want 1 flow, got %d", len(flows))
	}
	flow := flows[0]
	if flow.Name != "loginAndFetch" {
		t.Errorf("name = %q", flow.Name)
	}
	if flow.Description != "Logs in and fetches the profile" {
		t.Errorf("description = %q", flow.Description)
	}
	if len(flow.Inputs) != 2 || flow.Inputs[0].Name != "password" || flow.Inputs[1].Name != "username" {
		t.Fatalf("inputs = %+v", flow.Inputs)
	}
	if flow.Inputs[1].Value != "alice" {
		t.Errorf("username example = %v", flow.Inputs[1].Value)
	}
	if len(flow.Steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(flow.Steps))
	}

	login := flow.Steps[0]
	if login.StepID != "login" || login.OperationID != "loginUser" {
		t.Errorf("login step = %+v", login)
	}
	if len(login.Parameters) != 1 || login.Parameters[0].In != "query" || login.Parameters[0].Value != "$inputs.username" {
		t.Errorf("login params = %+v", login.Parameters)
	}
	body, ok := login.RequestBody.(map[string]any)
	if !ok || body["password"] != "$inputs.password" {
		t.Errorf("login body = %+v", login.RequestBody)
	}
	if len(login.Outputs) != 1 || login.Outputs[0].Name != "token" || login.Outputs[0].From != "$response.body#/token" {
		t.Errorf("login outputs = %+v", login.Outputs)
	}
	if len(login.SuccessCriteria) != 1 || login.SuccessCriteria[0].Condition != "$statusCode == 200" {
		t.Errorf("login criteria = %+v", login.SuccessCriteria)
	}

	profile := flow.Steps[1]
	if profile.Method != "GET" || profile.Path != "/users/{id}" {
		t.Errorf("profile operationPath not resolved: method=%q path=%q", profile.Method, profile.Path)
	}
	if len(profile.DependsOn) != 1 || profile.DependsOn[0] != "login" {
		t.Errorf("profile deps = %+v (expected [login] inferred from $steps.login)", profile.DependsOn)
	}
}

func TestParseJSON(t *testing.T) {
	const doc = `{"arazzo":"1.0.0","workflows":[{"workflowId":"w","steps":[{"stepId":"s1","method":"get","path":"/ping"}]}]}`
	flows, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if len(flows) != 1 || flows[0].Steps[0].Method != "GET" || flows[0].Steps[0].Path != "/ping" {
		t.Fatalf("unexpected json parse: %+v", flows)
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]byte("")); err == nil {
		t.Error("want error for empty input")
	}
	if _, err := Parse([]byte(`{"openapi":"3.0.0"}`)); err == nil {
		t.Error("want error when no workflows[]")
	}
}
