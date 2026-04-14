# ADR 002: Options for distributing API access to less technical users

**Date:** 2026-04-12
**Status:** Proposed — no decision made

---

## Context

The current setup requires users to create an Anthropic account, purchase credits, generate an API key, and set it as an environment variable before starting the app. This is a reasonable ask for a technical audience but creates friction for general users. The question is how to streamline API access without the operator absorbing all costs.

---

## Options

### Option 1: User-supplied key via the UI

Add a settings screen where users paste their Anthropic API key once. Store it in `localStorage`, send it as a request header on API calls, and have the server pass it through to Anthropic instead of reading from `ANTHROPIC_API_KEY`.

**Implementation touch points:**
- New settings UI panel in `web/index.html` + `web/static/js/app.js`
- Server reads the key from the request header when present, falls back to env var
- `internal/handlers/handlers.go` — pass the per-request key into `anthropic.New()`

**Pros:** Lowest implementation effort. Users still pay their own costs. No shell/env setup required — paste a key and go. Familiar pattern (Open WebUI, LibreChat, etc. use this approach).

**Cons:** Users still need an Anthropic account and credits. API key is stored in `localStorage` (cleared on browser data wipe, not shared across devices).

---

### Option 2: Operator hosts the key, tighten rate limiting

Keep the current model (operator pays), but reduce the rate limit from 10 req/sec to something like 5 identifications per IP per day to cap monthly exposure.

**Implementation touch points:**
- `internal/middleware/middleware.go` — change rate limiter from token bucket (per-second) to a daily counter (per-IP)
- This is a different algorithm than the current `golang.org/x/time/rate` approach

**Pros:** Zero friction for end users. Dead simple from a user experience standpoint.

**Cons:** Operator absorbs all API costs. Daily-counter rate limiting is stateful and more complex than the current token bucket. Scriptable — a determined user can rotate IPs.

**Cost ceiling estimate:** At Haiku 4.5 pricing (~$0.001 per identification), 5 req/day × 20 concurrent users = ~$3/month.

---

### Option 3: Thin paywall via Stripe

Users pay a small one-time or monthly fee (e.g. $1–2) via Stripe Checkout. After payment, the server issues them a bearer token (`API_KEY`) that gates access.

**Implementation touch points:**
- Stripe Checkout session creation + webhook handler (new routes)
- Token issuance and storage (extend `PlantStore` or a separate table)
- Frontend payment flow

**Pros:** Self-sustaining. Operator cost is covered with margin. Natural abuse deterrent (payment = accountability).

**Cons:** Significant implementation effort. Requires a public HTTPS domain (Stripe webhooks need a reachable endpoint). Adds payment infrastructure complexity and compliance considerations.

---

### Option 4: Swap to a model with a free tier (Google Gemini)

Replace the Anthropic backend with Google Gemini 2.0 Flash, which has a free tier with generous limits and supports vision input. Users need a Google account (free) to obtain an API key.

**Implementation touch points:**
- New `internal/gemini/client.go` implementing the `PlantIdentifier` interface
- `cmd/server/main.go` — swap `anthropic.New()` for `gemini.New()` based on config
- New env var (e.g. `GEMINI_API_KEY`)
- `go.mod` — add Google Generative AI Go SDK

**Pros:** Free tier may cover all personal use with no cost to the user. Google account is a lower barrier than an Anthropic account for most people.

**Cons:** Output quality may differ from Claude for structured JSON care plans — needs evaluation. Adds a second AI backend to maintain. Users still need to obtain and configure an API key (same friction, different provider).

---

### Option 5: Local inference via Ollama

Run a vision-capable model (e.g. LLaVA, Llama 3.2 Vision) locally via [Ollama](https://ollama.com). Zero external API costs or accounts.

**Implementation touch points:**
- New `internal/ollama/client.go` implementing `PlantIdentifier`
- Ollama uses a local HTTP API (`localhost:11434`) — no auth required
- `cmd/server/main.go` — select backend via `AI_BACKEND=ollama` env var

**Pros:** Completely offline. No accounts, no API keys, no costs. Good for privacy-conscious users.

**Cons:** Requires the user to install and run Ollama alongside the app — higher technical bar in a different way. Local model quality is noticeably lower than Claude for structured output tasks. Memory requirements (4–8 GB+ for a capable vision model) may exceed some users' hardware.

---

## Comparison

| Option | User cost | Operator cost | Setup friction | Implementation effort |
|--------|-----------|---------------|----------------|-----------------------|
| 1. UI key input | Pays own credits | None | Low (paste key) | Low |
| 2. Operator absorbs | None | Low (~$3/mo) | None | Medium |
| 3. Stripe paywall | Small one-time fee | None | Medium | High |
| 4. Gemini free tier | None (free tier) | None | Low (paste key) | Medium |
| 5. Ollama local | None | None | Medium (install Ollama) | Medium |

---

## Recommendation (if a decision were made today)

**Option 1** (UI key input) for a technical-adjacent audience — removes the env/shell friction while keeping costs with the user and requiring minimal code changes.

**Option 4** (Gemini free tier) if the goal is truly zero-cost and zero-account for end users — contingent on output quality being acceptable after evaluation.

No decision has been made. This document is for future reference.
