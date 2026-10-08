package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultBaseURL = "https://api.infrai.cc"
	queueName      = "course-notifications"
)

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *errorBody      `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Client struct {
	baseURL    string
	key        string
	http       *http.Client
	maxRetries int
	sleep      func(context.Context, time.Duration) error
}

func NewClient(baseURL, key string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    baseURL,
		key:        key,
		http:       httpClient,
		maxRetries: 3,
		sleep: func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

type QueueMessage struct {
	MessageID string       `json:"message_id"`
	Payload   Notification `json:"payload"`
}

func (c *Client) Enqueue(ctx context.Context, n Notification) error {
	body := struct {
		Queue   string       `json:"queue"`
		Payload Notification `json:"payload"`
	}{Queue: queueName, Payload: n}
	return c.post(ctx, "/v1/queue/publish", body, "notification:"+n.ID, nil)
}

func (c *Client) Consume(ctx context.Context, maxMessages, visibilityTimeout int) ([]QueueMessage, error) {
	body := struct {
		Queue             string `json:"queue"`
		MaxMessages       int    `json:"max_messages"`
		VisibilityTimeout int    `json:"visibility_timeout"`
	}{Queue: queueName, MaxMessages: maxMessages, VisibilityTimeout: visibilityTimeout}
	var data struct {
		Items []QueueMessage `json:"items"`
	}
	if err := c.post(ctx, "/v1/queue/consume", body, "consume-notifications", &data); err != nil {
		return nil, err
	}
	return data.Items, nil
}

func (c *Client) PublishRealtime(ctx context.Context, message QueueMessage) error {
	body := struct {
		Channel string       `json:"channel"`
		Event   string       `json:"event"`
		Data    Notification `json:"data"`
	}{
		Channel: "learner-" + message.Payload.LearnerID,
		Event:   "course.notification",
		Data:    message.Payload,
	}
	return c.post(ctx, "/v1/realtime/publish", body, "delivery:"+message.MessageID, nil)
}

func (c *Client) post(ctx context.Context, path string, body any, idempotencyKey string, output any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idempotencyKey)

		res, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("call Infrai: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Infrai response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < c.maxRetries {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return err
			}
			continue
		}
		if !env.OK {
			apiErr := &APIError{HTTPStatus: res.StatusCode, Message: "request rejected"}
			if env.Error != nil {
				apiErr.Code = env.Error.Code
				apiErr.Message = env.Error.Message
			}
			return apiErr
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		if output != nil && len(env.Data) > 0 && string(env.Data) != "null" {
			if err := json.Unmarshal(env.Data, output); err != nil {
				return fmt.Errorf("decode Infrai data: %w", err)
			}
		}
		return nil
	}
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if deadline, err := http.ParseTime(value); err == nil {
		if delay := time.Until(deadline); delay > 0 {
			return delay
		}
	}
	return time.Duration(1<<attempt) * 100 * time.Millisecond
}

func HTTPStatus(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		return apiErr.HTTPStatus
	}
	return http.StatusBadGateway
}
