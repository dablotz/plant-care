.PHONY: run build tidy docker-build docker-up docker-down

# Run locally (uses default AWS credential chain)
run:
	go run ./cmd/server

# Build binary locally
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o ./bin/plantcare ./cmd/server

# Sync go.sum
tidy:
	go mod tidy

# Build Docker image
docker-build:
	docker build -t plantcare:latest .

# Start with compose
docker-up:
	docker compose up --build -d

# Stop
docker-down:
	docker compose down

# View logs
logs:
	docker compose logs -f
