package openapi

import (
	"reflect"
	"testing"
)

const sampleSpec = `openapi: 3.1.0
info: {title: Test, version: "1.0"}
paths:
  /api/v1/users:
    post:
      operationId: createUser
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateUser'
      responses:
        "201":
          description: Created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/User'
        "422":
          description: Validation failed
    get:
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/User'
components:
  schemas:
    CreateUser:
      type: object
      required: [email]
      properties:
        email:
          type: string
          description: Login email
        nickname:
          type: string
        age:
          type: integer
        tags:
          type: array
          items: {type: string}
        profile:
          type: object
          properties:
            bio: {type: string}
    User:
      allOf:
        - $ref: '#/components/schemas/CreateUser'
        - type: object
          required: [id]
          properties:
            id: {type: string, format: uuid}
            createdAt: {type: string, format: date-time}
`

func TestEndpointsParsesOperationsInAuthoredOrder(t *testing.T) {
	endpoints, err := Endpoints(sampleSpec, "yaml")
	if err != nil {
		t.Fatalf("Endpoints returned error: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("endpoints = %d, want 2", len(endpoints))
	}

	// Methods come out in the fixed get/post/put/patch/delete order per path,
	// mirroring the frontend parser.
	post := endpoints[1]
	if post.Name != "createUser" || post.Method != "POST" || post.Path != "/api/v1/users" {
		t.Fatalf("post = %+v", post)
	}
	keys := make([]string, 0, len(post.Fields))
	for _, field := range post.Fields {
		keys = append(keys, field.Key)
	}
	if !reflect.DeepEqual(keys, []string{"email", "nickname", "age", "tags", "profile"}) {
		t.Fatalf("field order = %v", keys)
	}
	byKey := map[string]Field{}
	for _, field := range post.Fields {
		byKey[field.Key] = field
	}
	if !byKey["email"].Required || byKey["email"].Type != "string" || byKey["email"].Description == nil || *byKey["email"].Description != "Login email" {
		t.Fatalf("email = %+v", byKey["email"])
	}
	if byKey["age"].Type != "number" || byKey["tags"].Type != "string[]" {
		t.Fatalf("age/tags = %+v / %+v", byKey["age"], byKey["tags"])
	}
	if byKey["profile"].Type != "json" || byKey["profile"].Value == nil {
		t.Fatalf("profile = %+v", byKey["profile"])
	}

	if len(post.Responses) != 2 || post.Responses[0].Status != 201 || post.Responses[1].Status != 422 {
		t.Fatalf("responses = %+v", post.Responses)
	}
	created := map[string]Field{}
	for _, field := range post.Responses[0].Fields {
		created[field.Key] = field
	}
	// allOf merge: CreateUser properties + id/createdAt with formats mapped.
	if created["id"].Type != "uuid" || !created["id"].Required || created["createdAt"].Type != "timestamp" {
		t.Fatalf("201 fields = %+v", created)
	}
	if _, ok := created["email"]; !ok {
		t.Fatalf("allOf did not merge CreateUser properties: %+v", created)
	}
	if len(post.Responses[1].Fields) != 0 {
		t.Fatalf("422 should have no fields, got %+v", post.Responses[1].Fields)
	}

	get := endpoints[0]
	if get.Name != "GET /api/v1/users" || get.Method != "GET" {
		t.Fatalf("get = %+v", get)
	}
	// Array response collapses to a single json "data" field with a sample.
	if len(get.Responses) != 1 || len(get.Responses[0].Fields) != 1 {
		t.Fatalf("get responses = %+v", get.Responses)
	}
	data := get.Responses[0].Fields[0]
	if data.Key != "data" || data.Type != "json" || data.Value == nil {
		t.Fatalf("data field = %+v", data)
	}
}

func TestEndpointsJSONAndEdgeCases(t *testing.T) {
	spec := `{
  "openapi": "3.0.3",
  "paths": {
    "/things": {
      "get": {
        "responses": {
          "200": {"description": "OK"},
          "2XX": {"description": "range keys are skipped"},
          "default": {"description": "fallback"}
        }
      }
    }
  }
}`
	endpoints, err := Endpoints(spec, "json")
	if err != nil {
		t.Fatalf("Endpoints returned error: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("endpoints = %+v", endpoints)
	}
	statuses := []int{}
	for _, response := range endpoints[0].Responses {
		statuses = append(statuses, response.Status)
	}
	if !reflect.DeepEqual(statuses, []int{0, 200}) {
		t.Fatalf("statuses = %v (default first, 2XX dropped)", statuses)
	}
}

func TestEndpointsCycleSafe(t *testing.T) {
	spec := `openapi: 3.0.0
paths:
  /nodes:
    post:
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Node'
      responses:
        "200": {description: OK}
components:
  schemas:
    Node:
      type: object
      properties:
        name: {type: string}
        parent:
          $ref: '#/components/schemas/Node'
`
	endpoints, err := Endpoints(spec, "yaml")
	if err != nil {
		t.Fatalf("Endpoints returned error: %v", err)
	}
	if len(endpoints) != 1 || len(endpoints[0].Fields) != 2 {
		t.Fatalf("endpoints = %+v", endpoints)
	}
	parent := endpoints[0].Fields[1]
	if parent.Key != "parent" || parent.Type != "json" {
		t.Fatalf("parent = %+v", parent)
	}
}

func TestEndpointsEmptyPaths(t *testing.T) {
	endpoints, err := Endpoints("openapi: 3.1.0\ninfo: {title: T, version: v1}\npaths: {}\n", "yaml")
	if err != nil || len(endpoints) != 0 {
		t.Fatalf("endpoints = %+v err = %v", endpoints, err)
	}
}
