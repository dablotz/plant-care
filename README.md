# 🌿 PlantCare

A self-hosted houseplant care assistant. Identify any plant by name or photo,
get a full AI-powered care plan, and export care reminders to
**Apple Calendar / Outlook (.ics)** or **Google Calendar**.

Built in **Go** — compiles to a single static binary, runs in a ~15 MB distroless Docker image.

---

## Features

- **Plant identification** by name or photo (vision-capable AI)
- **Plant name validation** against the [GBIF](https://www.gbif.org/) species database before any AI call is made — catches typos and rejects non-plant input early
- **Structured care plans** — light, humidity, temperature, soil, schedule, toxicity notes, pro tips
- **Calendar export** — recurring `.ics` download or Google Calendar deep links, with per-task frequency overrides
- **Plant library** — save and reload care plans across sessions
- **Configurable AI backend** — switch between Anthropic, Google Gemini, and local Ollama at runtime via the Settings UI, no restart required

---

## Architecture

```
cmd/server/main.go          HTTP server entrypoint
internal/
  anthropic/client.go       Anthropic API (Claude Haiku 4.5) — vision + text
  gemini/client.go          Google Gemini 2.0 Flash — alternative backend
  ollama/client.go          Local Ollama — privacy-first, on-prem option
  gbif/client.go            GBIF species API — plant name validation
  calendar/ical.go          .ics generation + Google Calendar deep links
  handlers/handlers.go      HTTP handlers
  middleware/middleware.go   Auth, rate limiting, security headers
  models/models.go          Shared types
  settings/crypto.go        AES-256-GCM encryption for stored API keys
  store/
    store.go                PlantStore / SettingsStore interfaces
    sqlite.go               SQLite backend (local / Docker Compose)
    dynamo.go               DynamoDB backend (Lambda / cloud)
infra/                      Pulumi IaC — provisions Lambda + DynamoDB + S3
web/                        Vanilla HTML/CSS/JS frontend (served by Go)
Dockerfile                  Multi-stage build → distroless runtime
docker-compose.yml
```

---

## Quick Start

### Docker Compose (recommended)

```bash
make docker-up
# → http://localhost:8080
```

On first launch the Settings modal opens automatically. Enter your API key there —
it is stored AES-256-GCM encrypted in the SQLite database and never needs to be
set as an environment variable.

```bash
make logs             # follow container logs
make docker-down      # stop (data volume preserved)
make docker-down-clean  # stop and delete all saved plant data
```

### Local dev (no Docker)

```bash
make run-sqlite
# → http://localhost:8080
```

Open the app, click **⚙ Settings** in the top-right corner, and configure your backend.

---

## Configuring a Backend

Click **⚙ Settings** in the header at any time. The modal opens automatically on first
launch if no backend has been configured yet.

| Backend | What you need |
|---------|--------------|
| **Anthropic** | API key from [console.anthropic.com](https://console.anthropic.com) |
| **Google Gemini** | API key from [aistudio.google.com](https://aistudio.google.com) |
| **Ollama** | A running Ollama instance URL and a vision-capable model (e.g. `llava`) |

API keys are encrypted with AES-256-GCM before being written to the database.
They are never logged or returned to the client.

> **Note for Lambda deployments:** The settings store requires SQLite and is not
> available on Lambda. Configure the API key via Pulumi secrets instead (see
> [Cloud Deployment](#cloud-deployment-aws-lambda)).

---

## Security

- **Bearer token auth** — set `API_KEY` to require `Authorization: Bearer <token>` on all `/api/*` requests. If unset, auth is disabled (suitable for local use).
- **Rate limiting** — 10 requests/sec per IP, burst 20.
- **Request body limits** — 14 MB for identify, 64 KB for all other endpoints.
- **Image validation** — MIME type allowlist (`image/jpeg`, `image/png`, `image/webp`, `image/gif`) verified by magic bytes, not filename.
- **Input validation** — plant names limited to 200 characters, no control characters.
- **Plant name pre-screening** — GBIF species database is checked before any AI call; unrecognized names are rejected with a 422.
- **Encrypted key storage** — AI API keys are stored AES-256-GCM encrypted in SQLite.
- **S3 key prefix enforcement** — image upload keys must be under `uploads/`.
- **HTTP security headers** — `X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`, `Referrer-Policy`.
- **ICS injection prevention** — CRLF sequences stripped from all calendar output.

```bash
# Run with bearer token auth enabled
export API_KEY=your-secret-token
make run-sqlite
```

---

## Prerequisites

- Go 1.25+
- Docker (for containerised deployment)
- An account with at least one supported AI provider (Anthropic, Google Gemini, or a local Ollama installation)
- AWS credentials are **only required** for `STORAGE_TYPE=dynamodb` (cloud deployment)

---

## Storage

| `STORAGE_TYPE` | Backend | Use case |
|----------------|---------|----------|
| `sqlite` (default) | Local SQLite file | Local / Docker Compose |
| `dynamodb` | AWS DynamoDB | Lambda / cloud |

| Variable | Default | Description |
|----------|---------|-------------|
| `STORAGE_TYPE` | `sqlite` | `sqlite` or `dynamodb` |
| `SQLITE_PATH` | `/data/plantcare.db` | Path to SQLite file |
| `DYNAMODB_TABLE` | `plantcare-plants` | DynamoDB table name |

SQLite data is stored in a named Docker volume (`plantcare-data`) and persists
across container restarts, rebuilds, and host reboots. It is only deleted if
you explicitly run `docker-compose down -v` or `make docker-down-clean`.

---

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/identify` | Identify plant + get care plan. Accepts `{"name":"..."}` or `multipart/form-data` with `image` field |
| `POST` | `/api/calendar/ics` | Generate `.ics` file download |
| `POST` | `/api/calendar/google-links` | Get Google Calendar deep links by task |
| `POST` | `/api/plants` | Save a plant to the library |
| `GET`  | `/api/plants` | List all saved plants |
| `GET`  | `/api/plants/{id}` | Get a single saved plant |
| `DELETE` | `/api/plants/{id}` | Delete a saved plant |
| `GET`  | `/api/settings` | Get current backend configuration (keys are not returned) |
| `POST` | `/api/settings` | Update backend configuration |
| `GET`  | `/api/health` | Health check — returns `{"status":"ok"}` |

If `API_KEY` is set, add `-H "Authorization: Bearer <token>"` to all requests.

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

### Example: update backend settings
```bash
curl -X POST http://localhost:8080/api/settings \
  -H "Content-Type: application/json" \
  -d '{"active_backend": "anthropic", "anthropic_key": "sk-ant-..."}'
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

# 3. Provision AWS infrastructure and deploy
pulumi stack init dev
pulumi up --stack dev
# outputs: functionUrl, uploadBucket

# 4. Open the app
open $(cd infra && pulumi stack output functionUrl --stack dev)
```

Provisioned resources: Lambda function + Function URL, DynamoDB table, S3 upload bucket, IAM role, CloudWatch log group.

For subsequent code-only deploys:
```bash
make lambda-build && make lambda-deploy
```

### Minimum IAM policy (DynamoDB storage)

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

> **HTTPS:** The Lambda Function URL serves traffic over HTTPS automatically
> (AWS-managed certificate on `*.lambda-url.*.on.aws`). Custom domains also require HTTPS.

---

## Development

```bash
make test          # run unit tests
make lint          # run golangci-lint
make check         # vet + lint + test in one pass
make hooks-install # wire pre-commit hooks (run once per clone)
```

Pre-commit hooks run automatically on `git commit`: secrets scanning (detect-secrets), go vet, golangci-lint, and go test.

---

## Roadmap ideas

- [ ] User accounts with per-user plant libraries
- [ ] Watering log / care history and streak tracking
- [ ] Push notifications or email reminders
- [ ] Outlook Calendar deep links (alongside existing Google Calendar support)
- [ ] Plant health diagnostics from photo (yellowing, pests, root rot)
