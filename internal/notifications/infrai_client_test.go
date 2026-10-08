package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientUsesOneEndpointAndKeyForHandoff(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization header was not shared")
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Errorf("missing idempotency key")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/queue/consume" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"items": []any{map[string]any{
				"message_id": "msg-1", "payload": map[string]any{"id": "n-1", "learner_id": "l-1", "course_id": "c-1", "kind": "report", "title": "Report ready"},
			}}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{}})
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", server.Client())
	client.sleep = func(context.Context, time.Duration) error { return nil }
	n := Notification{ID: "n-1", LearnerID: "l-1", CourseID: "c-1", Kind: "report", Title: "Report ready"}
	if err := client.Enqueue(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	messages, err := client.Consume(context.Background(), 10, 60)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.PublishRealtime(context.Background(), messages[0]); err != nil {
		t.Fatal(err)
	}

	want := []string{"/v1/queue/publish", "/v1/queue/consume", "/v1/realtime/publish"}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("path[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}
