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
    dynamo.go               DynamoDB backend (Lambda / cloud deployment)
infra/                      Pulumi IaC — provisions Lambda + DynamoDB + S3
web/                        Vanilla HTML/CSS/JS frontend (served by Go)
Dockerfile                  Multi-stage build → distroless runtime
docker-compose.yml
```

---

## Security

The app ships with several hardening layers:

- **Bearer token auth** — set `API_KEY` to require `Authorization: Bearer <token>` on all `/api/*` requests. If unset, auth is disabled (dev mode).
- **Rate limiting** — 10 requests/sec per IP, burst 20, to protect against API cost abuse.
- **Request body limits** — JSON endpoints capped at 14 MB (identify) or 64 KB (all others).
- **Content-type allowlist** — image uploads only accept `image/jpeg`, `image/png`, `image/webp`, `image/gif` (validated by magic bytes, not filename).
- **Input validation** — plant names limited to 200 characters, no control characters.
- **S3 key prefix enforcement** — image keys must be under `uploads/`.
- **HTTP security headers** — `X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`, `Referrer-Policy` on every response.
- **ICS injection prevention** — CRLF sequences stripped from all calendar output.

```bash
# Run with auth enabled
export API_KEY=your-secret-token
make run-sqlite

# Then all API calls need:
curl -H "Authorization: Bearer your-secret-token" http://localhost:8080/api/health
```

---

## Prerequisites

- Go 1.24+
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
export API_KEY=your-secret-token   # optional — enables bearer token auth
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
| `dynamodb` | AWS DynamoDB | Lambda / cloud |

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

If `API_KEY` is set, add `-H "Authorization: Bearer <token>"` to all curl examples below.

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

## Cloud Deployment (AWS Lambda)

Infrastructure is managed with **Pulumi (Go)** in the `infra/` directory.

```bash
# 1. Store your Anthropic API key as a Pulumi secret
cd infra
pulumi config set --secret plantcare:anthropicApiKey sk-ant-...

# 2. Build the Lambda zip (Go binary + web/ static files)
make lambda-build

# 3. Provision AWS infrastructure and deploy initial code
pulumi stack init dev
pulumi up --stack dev
# outputs: functionUrl, uploadBucket

# 4. Open the app
open $(cd infra && pulumi stack output functionUrl --stack dev)
```

Provisioned resources: Lambda function + Function URL, DynamoDB table, S3 upload bucket, IAM role, CloudWatch log group.

For subsequent code-only deploys (faster than `pulumi up`):
```bash
make lambda-build
make lambda-deploy
```

To lock down S3 CORS to your frontend domain once you have one:
```bash
cd infra
pulumi config set plantcare:frontendOrigin https://your-domain.com
pulumi up --stack dev
```

> **Note:** The Lambda Function URL serves traffic over HTTPS automatically (AWS-managed certificate on `*.lambda-url.*.on.aws`). If you attach a custom domain, ensure it also uses HTTPS.

---

## Swapping Models

Edit `ModelID` in [internal/anthropic/client.go](internal/anthropic/client.go).
Use the model's API name (not the Bedrock cross-region profile ID).

| Model ID | Notes |
|----------|-------|
| `claude-haiku-4-5-20251001` | Default — fast, vision-capable |
| `claude-sonnet-4-6-20251101` | More capable, slower |

---

## Development

```bash
make test          # run unit tests
make lint          # run golangci-lint
make check         # vet + lint + test in one pass
make hooks-install # wire pre-commit hooks (run once per clone)
```

Pre-commit hooks run automatically on `git commit`: secrets scanning (detect-secrets), go vet, golangci-lint, and go test. The hooks are configured in [.pre-commit-config.yaml](.pre-commit-config.yaml).

---

## Roadmap ideas

- [ ] Watering log / streak tracking
- [ ] Push notifications via SNS
- [ ] HTTPS support on cloud ALB (requires a domain + ACM certificate)
- [ ] Terraform module for AWS App Runner deployment
