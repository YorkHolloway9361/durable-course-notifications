package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Notification struct {
	ID        string    `json:"id"`
	LearnerID string    `json:"learner_id"`
	CourseID  string    `json:"course_id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	DueAt     time.Time `json:"due_at"`
	Priority  string    `json:"priority"`
}

type DeliveryClient interface {
	Enqueue(context.Context, Notification) error
	Consume(context.Context, int, int) ([]QueueMessage, error)
	PublishRealtime(context.Context, QueueMessage) error
}

type Service struct {
	client DeliveryClient
	now    func() time.Time
}

func NewService(client DeliveryClient, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{client: client, now: now}
}

func (s *Service) Prepare(n Notification) (Notification, error) {
	if n.ID == "" || n.LearnerID == "" || n.CourseID == "" || n.Kind == "" || n.Title == "" {
		return Notification{}, fmt.Errorf("id, learner_id, course_id, kind, and title are required")
	}
	if n.Kind == "deadline" {
		hours := n.DueAt.Sub(s.now()).Hours()
		switch {
		case n.DueAt.IsZero():
			return Notification{}, fmt.Errorf("due_at is required for a deadline")
		case hours <= 24:
			n.Priority = "urgent"
		case hours <= 72:
			n.Priority = "soon"
		default:
			n.Priority = "normal"
		}
	} else {
		n.Priority = "normal"
	}
	return n, nil
}

func (s *Service) Enqueue(ctx context.Context, n Notification) (Notification, error) {
	prepared, err := s.Prepare(n)
	if err != nil {
		return Notification{}, err
	}
	if err := s.client.Enqueue(ctx, prepared); err != nil {
		return Notification{}, err
	}
	return prepared, nil
}

func (s *Service) Drain(ctx context.Context) (int, error) {
	messages, err := s.client.Consume(ctx, 10, 60)
	if err != nil {
		return 0, err
	}
	for i, message := range messages {
		if err := s.client.PublishRealtime(ctx, message); err != nil {
			return i, err
		}
	}
	return len(messages), nil
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /notifications", func(w http.ResponseWriter, r *http.Request) {
		var input Notification
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		queued, err := s.Enqueue(r.Context(), input)
		if err != nil {
			status := http.StatusBadRequest
			if _, ok := err.(*APIError); ok {
				status = HTTPStatus(err)
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, queued)
	})
	mux.HandleFunc("POST /worker/drain", func(w http.ResponseWriter, r *http.Request) {
		delivered, err := s.Drain(r.Context())
		if err != nil {
			writeJSON(w, HTTPStatus(err), map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"delivered": delivered})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
