# Contributing

This project is public as a portfolio piece. Pull requests and issue reports are not monitored — if you find it useful, **fork it and make it your own**.

## Forking and extending

The main extension points:

**Swap the AI model** — edit `ModelID` in [internal/anthropic/client.go](internal/anthropic/client.go). Any Claude model with vision support works. See the [Anthropic model list](https://docs.anthropic.com/en/docs/about-claude/models) for current IDs.

**Add a storage backend** — implement the `PlantStore` interface in [internal/store/store.go](internal/store/store.go) and wire it into the `switch storageType` block in [cmd/server/main.go](cmd/server/main.go).

**Swap the AI provider entirely** — implement the `PlantIdentifier` interface in [internal/handlers/handlers.go](internal/handlers/handlers.go) (`IdentifyAndPlan(ctx, req) (*CarePlan, error)`) and pass your implementation to the `Handler` struct.

## Local setup

```bash
export ANTHROPIC_API_KEY=sk-ant-...
make run-sqlite     # no Docker, no AWS needed
# → http://localhost:8080

make hooks-install  # wire pre-commit hooks (once per clone)
make check          # vet + lint + test
```
