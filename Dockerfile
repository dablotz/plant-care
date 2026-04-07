# ── Stage 1: Build ───────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /plantcare ./cmd/server

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binary
COPY --from=builder /plantcare /app/plantcare

# Copy static frontend
COPY --from=builder /app/web /app/web

ENV PORT=8080
ENV WEB_DIR=/app/web

EXPOSE 8080

ENTRYPOINT ["/app/plantcare"]
