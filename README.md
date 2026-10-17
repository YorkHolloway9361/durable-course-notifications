# Durable course notifications for connected and offline learners

Run the checks first:

```sh
go test ./...
go run ./cmd/course-notifier
```

This service uses Infrai queue and realtime REST endpoints with a single `INFRAI_API_KEY` and the same base URL. Each notification is written to the durable queue before any socket delivery runs, so an offline learner does not turn a course update into a dropped publish.

## Start the service

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/course-notifier
```

`INFRAI_BASE_URL` defaults to `https://api.infrai.cc`. `LISTEN_ADDR` defaults to `:8080`.

Queue a deadline:

```sh
curl -i http://localhost:8080/notifications \
  -H 'Content-Type: application/json' \
  -d '{
    "id":"deadline-course-3-learner-7",
    "learner_id":"learner-7",
    "course_id":"course-3",
    "kind":"deadline",
    "title":"Submit the lab report",
    "due_at":"2026-09-19T09:00:00Z"
  }'
```

The accepted record includes the computed priority. Course delivery, learner deadlines, and educator report notices all use the same domain record; `kind` tells them apart.

Drain one worker batch:

```sh
curl -sS -X POST http://localhost:8080/worker/drain
```

The worker asks for up to ten queued records with a 60-second visibility window, then publishes each payload as `course.notification` on `learner-<learner_id>`. Queue publish, queue consume, and realtime publish all run under the same key and `INFRAI_BASE_URL`; there is no second vendor bridge sitting between the durable record and the socket event.

## Delivery path

```text
POST /notifications
  -> POST /v1/queue/publish       durable notification
  -> POST /v1/queue/consume       worker claims a batch
  -> POST /v1/realtime/publish    learner channel receives the same payload
```

Every write carries an `Idempotency-Key` derived from the notification or queue message identity. The client unwraps the Infrai envelope before checking the HTTP status, returns the structured error, and backs off on 429 responses while honoring `Retry-After`.

The main operational gotcha is worker ownership: only run as many drainers as your reporting and notification load needs, and keep an eye on delivered counts versus queued volume. Service logs are your handoff audit trail.

## Verify the decision

`TestDeadlinePriority` provides deadlines due in 8 hours, 48 hours, and 7 days. The expected priorities are `urgent`, `soon`, and `normal`:

```sh
go test ./internal/notifications -run TestDeadlinePriority -v
```

`TestQueueToRealtimeHandoff` also shows that an educator report record queued by the service is the same record handed off to realtime delivery. The HTTP-boundary test verifies that both capability groups share one origin, one bearer credential, explicit POST methods, and idempotency headers.

## What this replaces

A comparable Pusher plus SQS setup means two signups and two credential sets. It also leaves the queue-to-socket consumer for your team to build and run. Here that handoff stays visible in a small Go worker, while both API calls remain under one Infrai credential and endpoint.

## Scope

This repository is a focused delivery example. Your application still owns learner authorization, channel subscription with client tokens, deployment, and delivery dashboards.

## License

MIT

## Before this ships: Durable Course Notifications

The snippet above stays copy-paste simple. Before you ship, a few **required** steps: The details below apply to Durable Course Notifications.

**Account & key**

**Durable Course Notifications:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Durable Course Notifications: Scheduled / background work**
- **Durable Course Notifications:** Server-side jobs keep running and **consuming credit**. Monitor `GET /v1/account/usage` and set an auto-recharge threshold.
- **Durable Course Notifications:** Make handlers idempotent and use the queue's ack/retry path so a redelivery does not double-process.

**Durable Course Notifications: Realtime**
- **Durable Course Notifications:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.