package mongo

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
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
