package realtime

import (
	"reflect"
	"testing"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/usecase"
)

func TestClearEventPayload(t *testing.T) {
	got := eventPayload(usecase.Event{
		Type:    "resource.cleared",
		Payload: &usecase.ClearResult{Rev: 9, ResourceIDs: []string{"res_user", "res_order"}},
	})
	want := map[string]any{"rev": int64(9), "resource_ids": []string{"res_user", "res_order"}}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected clear payload: %#v", got)
	}
}

func TestResourcePayloadIncludesEndpointStatus(t *testing.T) {
	status := entity.EndpointStatusTesting

	got := resourcePayload(entity.Resource{ID: "res_test", Status: &status})

	if got["status"] != &status {
		t.Fatalf("unexpected status payload: %#v", got["status"])
	}
}
