# 🌿 PlantCare

A self-hosted houseplant care assistant. Identify any plant by name or photo,
get a full care plan powered by **Claude Haiku 4.5 (Anthropic API)**, and export
care reminders to **Apple Calendar / Outlook (.ics)** or **Google Calendar**.

Built in **Go** — compiles to a single static binary, runs in a ~15 MB distroless Docker image.

---

## Architecture

```
cmd/server/main.go          HTTP server entrypoint
internal/
  anthropic/client.go       Anthropic API (Claude Haiku 4.5) — vision + text
  calendar/ical.go          .ics generation + Google Calendar deep links
  handlers/handlers.go      HTTP handlers
  models/models.go          Shared types
  store/
    store.go                PlantStore interface + PlantEntry type
    sqlite.go               SQLite backend (local / Docker Compose)
    dynamo.go               DynamoDB backend (AWS Fargate)
infra/                      Pulumi IaC — provisions the full AWS Fargate stack
web/                        Vanilla HTML/CSS/JS frontend (served by Go)
Dockerfile                  Multi-stage build → distroless runtime
docker-compose.yml
```

---

## Prerequisites

- Go 1.25+
- Docker (for containerized deployment)
- An **Anthropic API key** — create one at [console.anthropic.com](https://console.anthropic.com). The free tier is sufficient for personal use.
- AWS credentials are **only required** if using `STORAGE_TYPE=dynamodb` (cloud deployment)

---

## Quick Start

### Local dev (no Docker)

```bash
export ANTHROPIC_API_KEY=sk-ant-...
# Uses SQLite at ./plantcare.db
make run-sqlite
# → http://localhost:8080
```

### Docker Compose (recommended for local hosting)

```bash
export ANTHROPIC_API_KEY=sk-ant-...
make docker-up
# → http://localhost:8080

make logs        # follow container logs
make docker-down # stop (data volume is preserved)
make docker-down-clean  # stop AND delete saved plant data
```

SQLite data is stored in a named Docker volume (`plantcare-data`) and persists
across container restarts, rebuilds, and host reboots. It is only deleted if
you explicitly run `docker-compose down -v` or `make docker-down-clean`.

---

## Anthropic API Key

Set the `ANTHROPIC_API_KEY` environment variable before starting the app.

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

For Docker Compose the variable is read from your shell environment automatically.
Never commit the key to git.

## AWS Credentials (DynamoDB only)

AWS credentials are only needed when `STORAGE_TYPE=dynamodb`. The app uses the
standard Go AWS SDK credential chain (env vars → `~/.aws` → ECS task role).

For local use with DynamoDB, uncomment the `~/.aws` volume mount in
`docker-compose.yml`. For Fargate, the IAM task role handles credentials
automatically — no credential files needed.

### Minimum IAM policy (DynamoDB storage only)

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["dynamodb:PutItem", "dynamodb:GetItem", "dynamodb:DeleteItem", "dynamodb:Scan"],
      "Resource": "arn:aws:dynamodb:*:*:table/plantcare-plants"
    }
  ]
}
```

---

## Storage

The plant library backend is selected via the `STORAGE_TYPE` environment variable:

| `STORAGE_TYPE` | Backend | Use case |
|----------------|---------|----------|
| `sqlite` (default) | Local SQLite file | Local / Docker Compose |
| `dynamodb` | AWS DynamoDB | Fargate / cloud |

| Variable | Default | Description |
|----------|---------|-------------|
| `STORAGE_TYPE` | `sqlite` | `sqlite` or `dynamodb` |
| `SQLITE_PATH` | `/data/plantcare.db` | Path to SQLite file |
| `DYNAMODB_TABLE` | `plantcare-plants` | DynamoDB table name |

---

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/identify` | Identify plant + get care plan. Accepts JSON `{"name":"..."}` or `multipart/form-data` with `image` file field |
| `POST` | `/api/calendar/ics` | Generate `.ics` file download |
| `POST` | `/api/calendar/google-links` | Get Google Calendar deep links by task |
| `POST` | `/api/plants` | Save a plant to the library |
| `GET`  | `/api/plants` | List all saved plants |
| `GET`  | `/api/plants/{id}` | Get a single saved plant |
| `DELETE` | `/api/plants/{id}` | Delete a saved plant |
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

### Example: save to library
```bash
curl -X POST http://localhost:8080/api/plants \
  -H "Content-Type: application/json" \
  -d '{"care_plan": { ...care plan from /api/identify... }}'
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

## Cloud Deployment (AWS Fargate)

Infrastructure is managed with **Pulumi (Go)** in the `infra/` directory.

```bash
# 1. Store your Anthropic API key as a Pulumi secret
cd infra
pulumi config set --secret plantcare:anthropicApiKey sk-ant-...

# 2. Provision AWS infrastructure
pulumi stack init dev
pulumi up --stack dev
# outputs: albDnsName, ecrRepoUrl

# 3. Build and push the Docker image
make docker-push ECR_URL=<ecrRepoUrl> IMAGE_TAG=v1

# 4. Deploy the new image
cd infra
pulumi config set plantcare:imageTag v1
pulumi up --stack dev
```

Provisioned resources: ECR, ECS Fargate cluster, DynamoDB table, ALB, IAM roles, CloudWatch log group.

---

## Swapping Models

Edit `ModelID` in [internal/anthropic/client.go](internal/anthropic/client.go).
Use the model's API name (not the Bedrock cross-region profile ID).

| Model ID | Notes |
|----------|-------|
| `claude-haiku-4-5-20251001` | Default — fast, vision-capable |
| `claude-sonnet-4-6-20251101` | More capable, slower |

---

## Roadmap ideas

- [ ] Auth (single-token middleware)
- [ ] Watering log / streak tracking
- [ ] Push notifications via SNS
- [ ] Terraform module for AWS App Runner deployment
