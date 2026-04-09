.PHONY: run build tidy test vet docker-build docker-up docker-down logs run-sqlite infra-preview infra-up docker-push

# Run locally (uses default AWS credential chain)
run:
	go run ./cmd/server

# Run locally with SQLite storage (no Docker needed)
run-sqlite:
	STORAGE_TYPE=sqlite SQLITE_PATH=./plantcare.db go run ./cmd/server

# Build binary locally
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o ./bin/plantcare ./cmd/server

# Run tests
test:
	go test ./...

# Vet all packages
vet:
	go vet ./...

# Sync go.sum
tidy:
	go mod tidy

# Build Docker image
docker-build:
	docker build -t plantcare:latest .

# Start with compose
docker-up:
	docker compose up --build -d

# Stop (preserves data volume)
docker-down:
	docker compose down

# Stop and DELETE all data (removes the SQLite volume)
docker-down-clean:
	docker compose down -v

# View logs
logs:
	docker compose logs -f

# Pulumi preview (cloud infra dry-run)
infra-preview:
	cd infra && pulumi preview --stack dev

# Pulumi apply (provision/update cloud infra)
infra-up:
	cd infra && pulumi up --stack dev --yes

# Build and push image to ECR
# Usage: make docker-push ECR_URL=<ecr-repo-url> IMAGE_TAG=<tag>
docker-push:
	docker build -t $(ECR_URL):$(IMAGE_TAG) .
	docker push $(ECR_URL):$(IMAGE_TAG)
