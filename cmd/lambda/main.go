package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"

	"github.com/dablotz/plantcare/internal/anthropic"
	"github.com/dablotz/plantcare/internal/handlers"
	"github.com/dablotz/plantcare/internal/middleware"
	"github.com/dablotz/plantcare/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	region       := envOr("AWS_REGION", "us-east-1")
	uploadBucket := envOr("UPLOAD_BUCKET", "")
	webDir       := envOr("WEB_DIR", "/var/task/web")

	ctx := context.Background()

	apiKey := envOr("ANTHROPIC_API_KEY", "")
	if apiKey == "" {
		logger.Warn("ANTHROPIC_API_KEY not set — plant identification will fail")
	}
	aiClient := anthropic.New(apiKey)

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		logger.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}

	tableName  := envOr("DYNAMODB_TABLE", "plantcare-plants")
	plantStore := store.NewDynamoStore(ctx, cfg, tableName)
	s3Client   := s3.NewFromConfig(cfg)

	h := &handlers.Handler{
		Bedrock:      aiClient,
		Store:        plantStore,
		S3Client:     s3Client,
		UploadBucket: uploadBucket,
		Logger:       logger,
	}

	apiMux := http.NewServeMux()
	h.RegisterRoutes(apiMux)

	auth       := middleware.NewBearerAuth(logger)
	limiter    := middleware.NewRateLimiter(logger, 10, 20)
	apiHandler := limiter(auth(apiMux))

	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler)
	mux.Handle("/", http.FileServer(http.Dir(webDir)))

	handler := middleware.SecurityHeaders(mux)

	lambda.Start(httpadapter.NewV2(handler).ProxyWithContext)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
