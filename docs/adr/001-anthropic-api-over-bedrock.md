# ADR 001: Use Anthropic API directly instead of AWS Bedrock

**Date:** 2026-04-09
**Status:** Accepted

---

## Context

The app originally called Claude via **AWS Bedrock**, which requires:

1. An AWS account with Bedrock access enabled in a specific region
2. IAM credentials (`bedrock:InvokeModel` permission)
3. Managing credential expiry — SSO sessions time out, forcing re-authentication before the app will work
4. Every self-hoster needing their own AWS account, even though Bedrock free-tier access would technically be sufficient

This created friction for the primary operator (credential refresh interrupting local use) and a high barrier for anyone else wanting to self-host the app.

---

## Decision

Switch the plant identification backend from AWS Bedrock to the **Anthropic API** (`api.anthropic.com`) via the official [Anthropic Go SDK](https://github.com/anthropics/anthropic-sdk-go).

Authentication is a single `ANTHROPIC_API_KEY` environment variable. No AWS account, no IAM roles, no credential chain, no session expiry.

The switch is contained to one package (`internal/anthropic/client.go`) because the `PlantIdentifier` interface in `internal/handlers/handlers.go` already decoupled the AI backend from the rest of the app.

---

## Consequences

**Positive**

- Local Docker Compose no longer requires any AWS credentials at all (for the SQLite path). Set `ANTHROPIC_API_KEY` in the environment once and it never expires.
- Self-hosters only need an Anthropic account — no AWS account required. Anthropic's free tier covers light personal use.
- The IAM task role in the Fargate stack is simpler: only DynamoDB permissions remain, `bedrock:InvokeModel` is removed.
- Credential management for the Fargate deployment is cleaner: the API key is stored as a Pulumi secret (`pulumi config set --secret plantcare:anthropicApiKey`) and injected as a container environment variable.

**Negative / Trade-offs**

- The Anthropic API is an external SaaS endpoint. Bedrock kept traffic within the AWS network, which may matter for compliance-sensitive deployments. This app is a home-lab tool, so this is an acceptable trade-off.
- The Anthropic API key is a long-lived credential that must be protected. Unlike temporary IAM session tokens, a leaked key remains valid until manually revoked. Treat it as a secret: never commit it to git, use `pulumi config set --secret` for Fargate, and use environment variables (not files) locally.
- Pricing model changes: Bedrock and the Anthropic API price identically for the same models, but billing now goes through Anthropic rather than the AWS bill.

---

## Alternatives Considered

**Keep Bedrock, fix the credential problem with a long-lived IAM user key**
Would solve the personal expiry issue but keeps the AWS account requirement for self-hosters and doesn't address shareability.

**Keep Bedrock, mandate Fargate deployment (task role handles credentials)**
Eliminates credential management entirely but requires running infrastructure 24/7 and blocks local-only use cases.
