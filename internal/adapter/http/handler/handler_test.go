package handler

import (
	"encoding/json"
	"testing"
)

func TestUpdateFieldRequestDistinguishesMissingAndNullValue(t *testing.T) {
	var missing updateFieldRequest
	if err := json.Unmarshal([]byte(`{"state":"ready"}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Value.Set {
		t.Fatal("missing value was marked as set")
	}

	var null updateFieldRequest
	if err := json.Unmarshal([]byte(`{"value":null}`), &null); err != nil {
		t.Fatal(err)
	}
	if !null.Value.Set || null.Value.Value != nil {
		t.Fatalf("expected explicit null, got set=%v value=%#v", null.Value.Set, null.Value.Value)
	}

	var nested updateFieldRequest
	if err := json.Unmarshal([]byte(`{"value":{"profile":{"active":true},"tags":["new"]}}`), &nested); err != nil {
		t.Fatal(err)
	}
	if !nested.Value.Set {
		t.Fatal("nested value was not marked as set")
	}
}
