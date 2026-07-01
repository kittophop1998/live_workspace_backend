package mongo

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"

	"kingdom_manager/backend/internal/domain/entity"
)

func TestNormalizeBSONValuePreservesNestedJSONShape(t *testing.T) {
	input := bson.D{
		{Key: "profile", Value: bson.D{{Key: "active", Value: true}}},
		{Key: "tags", Value: bson.A{"new", bson.D{{Key: "rank", Value: int32(1)}}}},
	}
	want := map[string]any{
		"profile": map[string]any{"active": true},
		"tags":    []any{"new", map[string]any{"rank": int32(1)}},
	}

	if got := normalizeBSONValue(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected normalized value: %#v", got)
	}
}

func TestToEntityDefaultsLegacyEndpointStatusToDraft(t *testing.T) {
	workspace := toEntity(workspaceDocument{
		ID: "wsp_test",
		Resources: []resourceDocument{
			{ID: "res_endpoint", Kind: "endpoint"},
			{ID: "res_model", Kind: "model"},
		},
	})

	if workspace.Resources[0].Status == nil || *workspace.Resources[0].Status != entity.EndpointStatusDraft {
		t.Fatalf("expected legacy endpoint to default to draft, got %v", workspace.Resources[0].Status)
	}
	if workspace.Resources[1].Status != nil {
		t.Fatalf("expected model status to be nil, got %v", workspace.Resources[1].Status)
	}
}
