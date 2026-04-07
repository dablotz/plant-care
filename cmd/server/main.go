package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/dablotz/plantcare/internal/bedrock"
	"github.com/dablotz/plantcare/internal/handlers"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	port := envOr("PORT", "8080")
	region := envOr("AWS_REGION", "us-east-1")

	ctx := context.Background()

	logger.Info("starting plantcare server", "port", port, "aws_region", region)

	bedrockClient, err := bedrock.New(ctx, region)
	if err != nil {
		logger.Error("failed to initialize Bedrock client", "error", err)
		os.Exit(1)
	}

	h := &handlers.Handler{
		Bedrock: bedrockClient,
		Logger:  logger,
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
