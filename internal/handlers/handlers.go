package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/dablotz/plantcare/internal/calendar"
	"github.com/dablotz/plantcare/internal/models"
	"github.com/dablotz/plantcare/internal/store"
	"github.com/google/uuid"
)

// PlantIdentifier is the interface for plant identification backends.
type PlantIdentifier interface {
	IdentifyAndPlan(ctx context.Context, req models.PlantIdentifyRequest) (*models.CarePlan, error)
}

// Handler holds shared dependencies for HTTP handlers.
type Handler struct {
	Bedrock      PlantIdentifier
	Store        store.PlantStore // nil if storage is not configured
	S3Client     *s3.Client       // nil if S3 upload not configured
	UploadBucket string
	Logger       *slog.Logger
}

// RegisterRoutes wires up all routes to the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/identify", h.handleIdentify)
	mux.HandleFunc("POST /api/calendar/ics", h.handleICS)
	mux.HandleFunc("POST /api/calendar/google-links", h.handleGoogleLinks)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/config",         h.handleConfig)
	mux.HandleFunc("GET /api/upload-url",     h.handleUploadURL)
	mux.HandleFunc("POST /api/plants",        h.handleSavePlant)
	mux.HandleFunc("GET /api/plants",         h.handleListPlants)
	mux.HandleFunc("GET /api/plants/{id}",    h.handleGetPlant)
	mux.HandleFunc("DELETE /api/plants/{id}", h.handleDeletePlant)
}

// handleConfig returns frontend configuration flags.
func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	mode := "direct"
	if h.S3Client != nil && h.UploadBucket != "" {
		mode = "s3"
	}
	jsonOK(w, map[string]string{"image_upload_mode": mode})
}

// handleUploadURL returns a pre-signed S3 PUT URL and object key for a direct browser upload.
// Query param: content_type (e.g. "image/jpeg")
func (h *Handler) handleUploadURL(w http.ResponseWriter, r *http.Request) {
	if h.S3Client == nil || h.UploadBucket == "" {
		jsonError(w, "S3 upload not configured", http.StatusServiceUnavailable)
		return
	}

	contentType := r.URL.Query().Get("content_type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	key := "uploads/" + uuid.New().String()

	presigner := s3.NewPresignClient(h.S3Client)
	presigned, err := presigner.PresignPutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(h.UploadBucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		h.Logger.Error("presign put object", "error", err)
		jsonError(w, "failed to generate upload URL", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{
		"upload_url": presigned.URL,
		"key":        key,
	})
}

// handleIdentify accepts either a JSON body with a plant name, or a multipart
// form upload with an image file.
func (h *Handler) handleIdentify(w http.ResponseWriter, r *http.Request) {
	var req models.PlantIdentifyRequest

	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/form-data") {
		// Parse image upload (max 10MB)
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			jsonError(w, "parsing multipart form: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Optional text name alongside image
		req.Name = r.FormValue("name")

		file, header, err := r.FormFile("image")
		if err != nil && err != http.ErrMissingFile {
			jsonError(w, "reading image file: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err == nil {
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				jsonError(w, "reading image data: "+err.Error(), http.StatusInternalServerError)
				return
			}
			req.ImageBase64 = base64.StdEncoding.EncodeToString(data)
			req.ImageMIME = mimeFromFilename(header.Filename)
		}
	} else {
		// JSON body
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Fetch image from S3 if a key was provided instead of inline base64
		if req.ImageS3Key != "" {
			if h.S3Client == nil || h.UploadBucket == "" {
				jsonError(w, "S3 upload not configured", http.StatusServiceUnavailable)
				return
			}
			result, err := h.S3Client.GetObject(r.Context(), &s3.GetObjectInput{
				Bucket: aws.String(h.UploadBucket),
				Key:    aws.String(req.ImageS3Key),
			})
			if err != nil {
				h.Logger.Error("get s3 object", "key", req.ImageS3Key, "error", err)
				jsonError(w, "failed to retrieve uploaded image", http.StatusInternalServerError)
				return
			}
			defer result.Body.Close()
			data, err := io.ReadAll(result.Body)
			if err != nil {
				jsonError(w, "reading image from S3: "+err.Error(), http.StatusInternalServerError)
				return
			}
			req.ImageBase64 = base64.StdEncoding.EncodeToString(data)
			if req.ImageMIME == "" && result.ContentType != nil {
				req.ImageMIME = *result.ContentType
			}
		}
	}

	if req.Name == "" && req.ImageBase64 == "" {
		jsonError(w, "provide either 'name' or an image upload", http.StatusBadRequest)
		return
	}

	h.Logger.Info("identify request", "has_image", req.ImageBase64 != "", "name", req.Name)

	plan, err := h.Bedrock.IdentifyAndPlan(r.Context(), req)
	if err != nil {
		h.Logger.Error("bedrock identify", "error", err)
		jsonError(w, "plant identification failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, plan)
}

// handleICS generates and serves a .ics calendar file.
func (h *Handler) handleICS(w http.ResponseWriter, r *http.Request) {
	var req models.CalendarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	startDate, err := parseDate(req.StartDate)
	if err != nil {
		jsonError(w, "invalid start_date (use YYYY-MM-DD): "+err.Error(), http.StatusBadRequest)
		return
	}

	ics, err := calendar.GenerateICS(req.CarePlan, startDate, req.TaskOverrides)
	if err != nil {
		jsonError(w, "generating calendar: "+err.Error(), http.StatusInternalServerError)
		return
	}

	filename := sanitizeFilename(req.CarePlan.PlantName) + "-care.ics"
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(ics))
}

// handleGoogleLinks returns a map of task -> Google Calendar URL.
func (h *Handler) handleGoogleLinks(w http.ResponseWriter, r *http.Request) {
	var req models.CalendarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	startDate, err := parseDate(req.StartDate)
	if err != nil {
		jsonError(w, "invalid start_date (use YYYY-MM-DD): "+err.Error(), http.StatusBadRequest)
		return
	}

	links := calendar.GoogleCalendarLinks(req.CarePlan, startDate, req.TaskOverrides)
	jsonOK(w, links)
}

// --- plant library ---

func (h *Handler) handleSavePlant(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		jsonError(w, "storage not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		CarePlan models.CarePlan `json:"care_plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	entry := store.PlantEntry{
		ID:        uuid.New().String(),
		CreatedAt: time.Now().UTC(),
		CarePlan:  req.CarePlan,
	}
	if err := h.Store.SavePlant(r.Context(), entry); err != nil {
		h.Logger.Error("save plant", "error", err)
		jsonError(w, "failed to save plant", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(entry)
}

func (h *Handler) handleListPlants(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		jsonError(w, "storage not configured", http.StatusServiceUnavailable)
		return
	}
	entries, err := h.Store.ListPlants(r.Context())
	if err != nil {
		h.Logger.Error("list plants", "error", err)
		jsonError(w, "failed to list plants", http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []store.PlantEntry{} // return [] not null
	}
	jsonOK(w, entries)
}

func (h *Handler) handleGetPlant(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		jsonError(w, "storage not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	entry, err := h.Store.GetPlant(r.Context(), id)
	if err != nil {
		h.Logger.Error("get plant", "id", id, "error", err)
		jsonError(w, "failed to get plant", http.StatusInternalServerError)
		return
	}
	if entry == nil {
		jsonError(w, "plant not found", http.StatusNotFound)
		return
	}
	jsonOK(w, entry)
}

func (h *Handler) handleDeletePlant(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		jsonError(w, "storage not configured", http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := h.Store.DeletePlant(r.Context(), id); err != nil {
		h.Logger.Error("delete plant", "id", id, "error", err)
		jsonError(w, "failed to delete plant", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Now(), nil
	}
	return time.Parse("2006-01-02", s)
}

func mimeFromFilename(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

func sanitizeFilename(s string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			out.WriteRune(r)
		} else {
			out.WriteRune('-')
		}
	}
	return strings.Trim(out.String(), "-")
}
