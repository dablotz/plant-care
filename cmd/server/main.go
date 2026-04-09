package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/dablotz/plantcare/internal/anthropic"
	"github.com/dablotz/plantcare/internal/handlers"
	"github.com/dablotz/plantcare/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	port := envOr("PORT", "8080")
	region := envOr("AWS_REGION", "us-east-1")

	ctx := context.Background()

	logger.Info("starting plantcare server", "port", port, "aws_region", region)

	apiKey := envOr("ANTHROPIC_API_KEY", "")
	if apiKey == "" {
		logger.Warn("ANTHROPIC_API_KEY not set — plant identification will fail")
	}
	aiClient := anthropic.New(apiKey)

	var plantStore store.PlantStore
	var s3Client *s3.Client
	uploadBucket := envOr("UPLOAD_BUCKET", "")

	switch storageType := envOr("STORAGE_TYPE", "sqlite"); storageType {
	case "sqlite":
		sqlitePath := envOr("SQLITE_PATH", "/data/plantcare.db")
		s, err := store.NewSQLiteStore(ctx, sqlitePath)
		if err != nil {
			logger.Error("failed to initialize SQLite store", "error", err)
			os.Exit(1)
		}
		plantStore = s
		logger.Info("using SQLite store", "path", sqlitePath)
	case "dynamodb":
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
		if err != nil {
			logger.Error("failed to load AWS config for DynamoDB", "error", err)
			os.Exit(1)
		}
		tableName := envOr("DYNAMODB_TABLE", "plantcare-plants")
		plantStore = store.NewDynamoStore(ctx, cfg, tableName)
		logger.Info("using DynamoDB store", "table", tableName)

		if uploadBucket != "" {
			s3Client = s3.NewFromConfig(cfg)
			logger.Info("S3 image uploads enabled", "bucket", uploadBucket)
		}
	default:
		logger.Warn("unknown STORAGE_TYPE, plant library disabled", "storage_type", storageType)
	}

	h := &handlers.Handler{
		Bedrock:      aiClient,
		Store:        plantStore,
		S3Client:     s3Client,
		UploadBucket: uploadBucket,
		Logger:       logger,
	}

	mux := http.NewServeMux()

	// API routes
	h.RegisterRoutes(mux)

	// Serve static frontend files
	webDir := envOr("WEB_DIR", "./web")
	absWebDir, _ := filepath.Abs(webDir)
	logger.Info("serving frontend", "dir", absWebDir)
	mux.Handle("/", http.FileServer(http.Dir(webDir)))

	// Wrap with request logging middleware
	logged := requestLogger(logger, mux)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      logged,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 120 * time.Second, // longer for Bedrock calls
		IdleTimeout:  120 * time.Second,
	}

	logger.Info("server listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.code,
			"duration_ms", fmt.Sprintf("%.2f", float64(time.Since(start).Microseconds())/1000),
			"remote_addr", r.RemoteAddr,
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	code int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.code = code
	rw.ResponseWriter.WriteHeader(code)
}
