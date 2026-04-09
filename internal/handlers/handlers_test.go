package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dablotz/plantcare/internal/models"
)

// mockBedrock is a test double for PlantIdentifier.
type mockBedrock struct {
	plan *models.CarePlan
	err  error
}

func (m *mockBedrock) IdentifyAndPlan(_ context.Context, _ models.PlantIdentifyRequest) (*models.CarePlan, error) {
	return m.plan, m.err
}

func newTestHandler(mock PlantIdentifier) (*Handler, *http.ServeMux) {
	h := &Handler{
		Bedrock: mock,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

// --- health ---

func TestHealthHandler(t *testing.T) {
	_, mux := newTestHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf(`expected {"status":"ok"}, got %v`, body)
	}
}

// --- /api/calendar/ics ---

func TestHandleICS_ValidRequest(t *testing.T) {
	_, mux := newTestHandler(nil)

	reqBody := models.CalendarRequest{
		CarePlan: models.CarePlan{
			PlantName: "Monstera",
			Schedule: []models.CareScheduleItem{
				{Task: "Watering", FrequencyDays: 7},
			},
		},
		StartDate: "2024-06-01",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/calendar/ics", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/calendar") {
		t.Errorf("expected text/calendar content type, got %q", ct)
	}
	if !strings.Contains(w.Body.String(), "BEGIN:VCALENDAR") {
		t.Error("expected ICS content in response body")
	}
}

func TestHandleICS_InvalidDate(t *testing.T) {
	_, mux := newTestHandler(nil)

	reqBody := models.CalendarRequest{
		CarePlan:  models.CarePlan{PlantName: "Monstera"},
		StartDate: "not-a-date",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/calendar/ics", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleICS_InvalidJSON(t *testing.T) {
	_, mux := newTestHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/calendar/ics", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleICS_ContentDispositionFilename(t *testing.T) {
	_, mux := newTestHandler(nil)

	reqBody := models.CalendarRequest{
		CarePlan:  models.CarePlan{PlantName: "Snake Plant", Schedule: []models.CareScheduleItem{{Task: "Watering", FrequencyDays: 7}}},
		StartDate: "2024-06-01",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/calendar/ics", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "snake-plant-care.ics") {
		t.Errorf("expected sanitized filename in Content-Disposition, got %q", cd)
	}
}

// --- /api/calendar/google-links ---

func TestHandleGoogleLinks_ValidRequest(t *testing.T) {
	_, mux := newTestHandler(nil)

	reqBody := models.CalendarRequest{
		CarePlan: models.CarePlan{
			PlantName: "Snake Plant",
			Schedule: []models.CareScheduleItem{
				{Task: "Watering", FrequencyDays: 14},
			},
		},
		StartDate: "2024-06-01",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/calendar/google-links", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var links map[string]string
	json.NewDecoder(w.Body).Decode(&links)
	if _, ok := links["Watering"]; !ok {
		t.Error("expected Watering link in response")
	}
}

func TestHandleGoogleLinks_InvalidDate(t *testing.T) {
	_, mux := newTestHandler(nil)

	reqBody := models.CalendarRequest{
		CarePlan:  models.CarePlan{PlantName: "Cactus"},
		StartDate: "bad-date",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/calendar/google-links", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- /api/identify ---

func TestHandleIdentify_ValidJSON(t *testing.T) {
	mock := &mockBedrock{
		plan: &models.CarePlan{
			PlantName: "Monstera deliciosa",
			Schedule:  []models.CareScheduleItem{{Task: "Watering", FrequencyDays: 7}},
		},
	}
	_, mux := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPost, "/api/identify", strings.NewReader(`{"name":"Monstera deliciosa"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var plan models.CarePlan
	json.NewDecoder(w.Body).Decode(&plan)
	if plan.PlantName != "Monstera deliciosa" {
		t.Errorf("unexpected plant name: %q", plan.PlantName)
	}
}

func TestHandleIdentify_MissingInput(t *testing.T) {
	_, mux := newTestHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/identify", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleIdentify_BedrockError(t *testing.T) {
	mock := &mockBedrock{err: errors.New("service unavailable")}
	_, mux := newTestHandler(mock)

	req := httptest.NewRequest(http.MethodPost, "/api/identify", strings.NewReader(`{"name":"Monstera"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
}

func TestHandleIdentify_InvalidJSON(t *testing.T) {
	_, mux := newTestHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/identify", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// --- helper functions ---

func TestParseDate(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"2024-06-01", false},
		{"2000-01-01", false},
		{"", false}, // empty returns time.Now(), not an error
		{"not-a-date", true},
		{"2024-13-01", true},
	}
	for _, tt := range tests {
		_, err := parseDate(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseDate(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
	}
}

func TestMimeFromFilename(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"plant.jpg", "image/jpeg"},
		{"plant.jpeg", "image/jpeg"},
		{"plant.png", "image/png"},
		{"plant.gif", "image/gif"},
		{"plant.webp", "image/webp"},
		{"plant.PNG", "image/png"},  // case-insensitive
		{"plant.bmp", "image/jpeg"}, // unknown -> jpeg
		{"noextension", "image/jpeg"},
	}
	for _, tt := range tests {
		got := mimeFromFilename(tt.filename)
		if got != tt.want {
			t.Errorf("mimeFromFilename(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Monstera", "monstera"},
		{"Snake Plant", "snake-plant"},
		{"Fiddle-Leaf Fig", "fiddle-leaf-fig"},
		{"123plant", "123plant"},
		{"  spaces  ", "spaces"},
		{"special!@#chars", "special---chars"},
	}
	for _, tt := range tests {
		got := sanitizeFilename(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
