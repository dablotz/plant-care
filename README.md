# 🌿 PlantCare

A self-hosted houseplant care assistant. Identify any plant by name or photo,
get a full care plan powered by **AWS Bedrock (Claude 3 Sonnet)**, and export
care reminders to **Apple Calendar / Outlook (.ics)** or **Google Calendar**.

Built in **Go** — compiles to a single static binary, runs in a ~15 MB distroless Docker image.

---

## Architecture

```
cmd/server/main.go          HTTP server entrypoint
internal/
  bedrock/client.go         AWS Bedrock (Claude 3 Sonnet) — vision + text
  calendar/ical.go          .ics generation + Google Calendar deep links
  handlers/handlers.go      HTTP handlers
  models/models.go          Shared types
web/                        Vanilla HTML/CSS/JS frontend (served by Go)
Dockerfile                  Multi-stage build → distroless runtime
docker-compose.yml
```

---

## Prerequisites

- Go 1.22+
- Docker (for containerized deployment)
- AWS account with **Bedrock access enabled** in your chosen region
  - Request access to **Claude 3 Sonnet** (`anthropic.claude-3-sonnet-20240229-v1:0`) in the Bedrock console
- AWS credentials with `bedrock:InvokeModel` permission

---

## Quick Start

### Local dev

```bash
cp .env.example .env
# fill in AWS_REGION and credentials

go mod download
make run
# → http://localhost:8080
```

### Docker Compose

```bash
cp .env.example .env
# fill in values

make docker-up
# → http://localhost:8080

make logs     # follow container logs
make docker-down
```

---

## AWS Credentials

The app uses the standard Go AWS SDK credential chain, in priority order:

1. **Environment variables** — `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`
2. **Named profile** — `AWS_PROFILE=myprofile` with `~/.aws/credentials`
3. **EC2/ECS instance role** — automatic if deployed on AWS compute
4. **IAM Identity Center (SSO)** — if configured in `~/.aws/config`

The `docker-compose.yml` mounts `~/.aws` read-only into the container so
profiles work out of the box for home lab use. For production, prefer IAM roles
or parameter store injection.

### Minimum IAM policy

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["bedrock:InvokeModel"],
      "Resource": "arn:aws:bedrock:*::foundation-model/anthropic.claude-3-sonnet-20240229-v1:0"
    }
  ]
}
```

---

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/identify` | Identify plant + get care plan. Accepts JSON `{"name":"..."}` or `multipart/form-data` with `image` file field |
| `POST` | `/api/calendar/ics` | Generate `.ics` file download |
| `POST` | `/api/calendar/google-links` | Get Google Calendar deep links by task |
| `GET`  | `/api/health` | Health check |

### Example: identify by name
```bash
curl -X POST http://localhost:8080/api/identify \
  -H "Content-Type: application/json" \
  -d '{"name": "Monstera deliciosa"}'
```

### Example: identify by image
```bash
curl -X POST http://localhost:8080/api/identify \
  -F "image=@/path/to/plant.jpg"
```

### Example: download .ics
```bash
curl -X POST http://localhost:8080/api/calendar/ics \
  -H "Content-Type: application/json" \
  -d '{
    "care_plan": { ...care plan from /api/identify... },
    "start_date": "2024-06-01",
    "task_overrides": {
      "Watering": 7,
      "Fertilizing": 30,
      "Repotting": 0
    }
  }' -o plant-care.ics
```

---

## Swapping Models

Edit `ModelID` in `internal/bedrock/client.go`:

| Model | Notes |
|-------|-------|
| `anthropic.claude-3-sonnet-20240229-v1:0` | Default — balanced |
| `anthropic.claude-3-haiku-20240307-v1:0` | Faster, cheaper |
| `anthropic.claude-3-opus-20240229-v1:0` | Most capable |

---

## Roadmap ideas

- [ ] Auth (single-token middleware — stub already planned)
- [ ] Plant library: save identified plants to SQLite
- [ ] Watering log / streak tracking
- [ ] Push notifications via SNS (ties into Cost Sentinel SNS patterns)
- [ ] Terraform module for AWS App Runner deployment
