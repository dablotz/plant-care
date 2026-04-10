.PHONY: run build tidy test vet lint check hooks-install docker-build docker-up docker-down logs run-sqlite infra-preview infra-up lambda-build lambda-deploy

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

# Run golangci-lint
lint:
	golangci-lint run

# Run all code quality checks (vet, lint, test) — fast and pre-commit friendly
check:
	go vet ./...
	golangci-lint run
	go test ./...

# Install pre-commit hooks into .git/hooks/ (run once per clone)
hooks-install:
	pre-commit install

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

# Build Lambda zip (linux/amd64 binary + web/ directory)
lambda-build:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bootstrap ./cmd/lambda
	zip -r plantcare-lambda.zip bootstrap web/
	rm bootstrap

# Deploy updated Lambda code (faster than pulumi up for code-only changes)
lambda-deploy:
	aws lambda update-function-code \
		--function-name plantcare \
		--zip-file fileb://plantcare-lambda.zip \
		--region $(shell cd infra && pulumi config get aws:region 2>/dev/null || echo us-east-1)
