package notifications

import (
	"context"
	"testing"
	"time"
)

type recordingClient struct {
	queued    []Notification
	messages  []QueueMessage
	published []QueueMessage
}

func (c *recordingClient) Enqueue(_ context.Context, n Notification) error {
	c.queued = append(c.queued, n)
	return nil
}

func (c *recordingClient) Consume(context.Context, int, int) ([]QueueMessage, error) {
	return c.messages, nil
}

func (c *recordingClient) PublishRealtime(_ context.Context, message QueueMessage) error {
	c.published = append(c.published, message)
	return nil
}

func TestDeadlinePriority(t *testing.T) {
	now := time.Date(2026, time.September, 18, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		due  time.Time
		want string
	}{
		{name: "due today", due: now.Add(8 * time.Hour), want: "urgent"},
		{name: "due in two days", due: now.Add(48 * time.Hour), want: "soon"},
		{name: "due next week", due: now.Add(7 * 24 * time.Hour), want: "normal"},
	}

	service := NewService(&recordingClient{}, func() time.Time { return now })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.Prepare(Notification{
				ID: "deadline-42", LearnerID: "learner-7", CourseID: "course-3",
				Kind: "deadline", Title: "Submit lab report", DueAt: tt.due,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.Priority != tt.want {
				t.Fatalf("priority = %q, want %q", got.Priority, tt.want)
			}
		})
	}
}

func TestQueueToRealtimeHandoff(t *testing.T) {
	n := Notification{ID: "report-ready-9", LearnerID: "learner-7", CourseID: "course-3", Kind: "report", Title: "Weekly progress is ready"}
	client := &recordingClient{messages: []QueueMessage{{MessageID: "message-9", Payload: n}}}
	service := NewService(client, nil)

	queued, err := service.Enqueue(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	if queued.Priority != "normal" || len(client.queued) != 1 {
		t.Fatalf("queued notification = %#v", queued)
	}
	delivered, err := service.Drain(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if delivered != 1 || len(client.published) != 1 || client.published[0].Payload.ID != n.ID {
		t.Fatalf("handoff delivered=%d published=%#v", delivered, client.published)
	}
}
