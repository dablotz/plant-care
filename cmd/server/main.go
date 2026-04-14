package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/dablotz/plantcare/internal/handlers"
	"github.com/dablotz/plantcare/internal/middleware"
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

	encryptionKey := envOr("ENCRYPTION_KEY", "")
	if encryptionKey == "" {
		logger.Warn("ENCRYPTION_KEY not set — API keys will be stored unencrypted")
	}

	isLambda := os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != ""

	var plantStore store.PlantStore
	var settingsStore store.SettingsStore
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
		settingsStore = s
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
		SettingsStore: settingsStore,
		EncryptionKey: encryptionKey,
		IsLambda:      isLambda,
		Store:         plantStore,
		S3Client:      s3Client,
		UploadBucket:  uploadBucket,
		Logger:        logger,
	}

	// API sub-mux: auth + rate limiting applied here only
	apiMux := http.NewServeMux()
	h.RegisterRoutes(apiMux)

	auth := middleware.NewBearerAuth(logger)
	limiter := middleware.NewRateLimiter(logger, 10, 20)
	apiHandler := limiter(auth(apiMux))

	// Top-level mux: /api/* gets secured handler, / gets static files
	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler)

	// Serve static frontend files
	webDir := envOr("WEB_DIR", "./web")
	if _, err := os.Stat(webDir); err != nil {
		logger.Error("WEB_DIR does not exist or is not accessible", "dir", webDir, "error", err)
		os.Exit(1)
	}
	logger.Info("serving frontend", "dir", webDir)
	mux.Handle("/", http.FileServer(http.Dir(webDir)))

	// Security headers wrap everything; request logger is outermost
	logged := requestLogger(logger, middleware.SecurityHeaders(mux))

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
