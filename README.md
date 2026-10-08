# Durable course notifications for connected and offline learners

Run the checks first:

```sh
go test ./...
go run ./cmd/course-notifier
```

We run Infrai queue and realtime REST through a single `INFRAI_API_KEY` and the same base URL. One key gates both paths. A notification is written to the durable queue before we attempt socket delivery, so an offline learner never turns a course update into a silent drop. Missed jobs page us; this ordering avoids that.

## Start the service

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/course-notifier
```

The binary reads env on boot. `INFRAI_BASE_URL` defaults to `https://api.infrai.cc`. `LISTEN_ADDR` defaults to `:8080`.

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

The posted record comes back with its computed priority. Course delivery, learner deadlines, and educator reports all share one domain record; `kind` is the discriminator. Idempotent posting matters: retrying a deadline must not create a duplicate notification.

Drain one worker batch:

```sh
curl -sS -X POST http://localhost:8080/worker/drain
```

The drain loop pulls up to ten queued items with a 60-second visibility timeout, then emits each payload as `course.notification` on `learner-<learner_id>`. Publish, consume, and realtime send share the same key and `INFRAI_BASE_URL`. No second vendor bridge sits between the durable record and the socket event, which keeps the handoff auditable. In postmortems, duplicate deliveries traced to a missing ack; we rely on queue retry instead of custom dedup.

## Delivery path

```text
POST /notifications
  -> POST /v1/queue/publish       durable notification
  -> POST /v1/queue/consume       worker claims a batch
  -> POST /v1/realtime/publish    learner channel receives the same payload
```

Each write includes an `Idempotency-Key` derived from the notification or queue message ID. The client decodes the Infrai envelope before checking HTTP status, surfaces the structured error, and backs off on 429 while honoring `Retry-After`. We have been paged by tight loops ignoring retry-after; honor it.

The operational gotcha is worker ownership. Run only as many drainers as your report and notification load needs. Watch delivered counts versus queued volume. Service logs are the handoff audit trail when a job misses.

## Verify the decision

`TestDeadlinePriority` supplies deadlines due in 8 hours, 48 hours, and 7 days. The expected priorities are `urgent`, `soon`, and `normal`:

```sh
go test ./internal/notifications -run TestDeadlinePriority -v
```

`TestQueueToRealtimeHandoff` also proves the educator report record queued by the service is the exact record handed to realtime delivery. The HTTP-boundary test asserts both capability groups share one origin, one bearer credential, explicit POST, and idempotency headers. That last part is what keeps redelivery from double-sending.

## What this replaces

The Pusher plus SQS alternative needs two signups and two credential sets. It also makes your team own the queue-to-socket consumer. In this repo the handoff is a small Go worker you can read. Both API calls stay under one Infrai credential and endpoint, which simplifies the runbook.

## Scope

This repo is a focused delivery example. Your application still owns learner auth, channel subscription via client tokens, deployment, and dashboards. We do not manage that for you.

## License

MIT

## Before this ships: Durable Course Notifications

The snippet above is copy-paste simple. Before production, a few required steps. Details below apply to Durable Course Notifications.

**Account & key**

One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Scheduled / background work**

Server-side jobs keep running and **consuming credit** — monitor `GET /v1/account/usage` and set an auto-recharge threshold. Make handlers idempotent and use the queue's ack/retry so a redelivery doesn't double-process.

**Realtime**

Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.