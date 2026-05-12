# Contributing

This project is public as a portfolio piece. Pull requests and issue reports are not monitored — if you find it useful, **fork it and make it your own**.

## Local setup

```bash
make run-sqlite     # no Docker, no AWS needed
# → http://localhost:8080

make hooks-install  # wire pre-commit hooks (once per clone)
make check          # vet + lint + test
```

On first launch the Settings modal opens automatically. Enter your API key for whichever backend you want to use — it is stored encrypted in the local SQLite database. No environment variables needed.

You can reopen Settings at any time with the **⚙ Settings** button in the top-right corner of the app.

## Forking and extending

**Switch AI backends at runtime** — the Settings UI supports Anthropic, Google Gemini, and local Ollama out of the box. Switching takes effect immediately without a restart.

**Add a new AI backend** — implement the `PlantIdentifier` interface in [internal/handlers/handlers.go](internal/handlers/handlers.go):

```go
IdentifyAndPlan(ctx context.Context, req models.PlantIdentifyRequest) (*models.CarePlan, error)
```

Follow the pattern in [internal/anthropic/client.go](internal/anthropic/client.go). Wire your backend into `resolveBackend` in the same file and add it to the Settings UI dropdown in [web/index.html](web/index.html).

If the model cannot identify the input, return `&models.IdentifyError{Message: "..."}` — the handler will surface this as a 422 rather than a 500.

**Add a storage backend** — implement the `PlantStore` interface in [internal/store/store.go](internal/store/store.go) and wire it into the `switch storageType` block in [cmd/server/main.go](cmd/server/main.go).
