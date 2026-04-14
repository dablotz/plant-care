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
	"time"

	"github.com/dablotz/plantcare/internal/models"
	"github.com/dablotz/plantcare/internal/store"
)

// mockAI is a test double for PlantIdentifier.
type mockAI struct {
	plan *models.CarePlan
	err  error
}

func (m *mockAI) IdentifyAndPlan(_ context.Context, _ models.PlantIdentifyRequest) (*models.CarePlan, error) {
	return m.plan, m.err
}

// mockStore is a test double for store.PlantStore.
type mockStore struct {
	entries []store.PlantEntry
	saveErr error
}

func (m *mockStore) SavePlant(_ context.Context, entry store.PlantEntry) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.entries = append(m.entries, entry)
	return nil
}
func (m *mockStore) ListPlants(_ context.Context) ([]store.PlantEntry, error) {
	return m.entries, nil
}
func (m *mockStore) GetPlant(_ context.Context, id string) (*store.PlantEntry, error) {
	for i, e := range m.entries {
		if e.ID == id {
			return &m.entries[i], nil
		}
	}
	return nil, nil
}
func (m *mockStore) DeletePlant(_ context.Context, id string) error {
	for i, e := range m.entries {
		if e.ID == id {
			m.entries = append(m.entries[:i], m.entries[i+1:]...)
			return nil
		}
	}
	return nil
}

func newTestHandler(mock PlantIdentifier) (*Handler, *http.ServeMux) {
	h := &Handler{
		backendOverride: mock,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

func newTestHandlerWithStore(mock PlantIdentifier, s store.PlantStore) (*Handler, *http.ServeMux) {
	h := &Handler{
		backendOverride: mock,
		Store:           s,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
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
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
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
	if err := json.NewDecoder(w.Body).Decode(&links); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
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
	mock := &mockAI{
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
	if err := json.NewDecoder(w.Body).Decode(&plan); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
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
	mock := &mockAI{err: errors.New("service unavailable")}
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

// --- /api/plants ---

func TestHandleSavePlant(t *testing.T) {
	ms := &mockStore{}
	_, mux := newTestHandlerWithStore(nil, ms)

	body, _ := json.Marshal(map[string]interface{}{
		"care_plan": models.CarePlan{PlantName: "Monstera"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/plants", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(ms.entries) != 1 {
		t.Errorf("expected 1 saved entry, got %d", len(ms.entries))
	}
}

func TestHandleSavePlant_NoStore(t *testing.T) {
	_, mux := newTestHandler(nil) // Store is nil

	body, _ := json.Marshal(map[string]interface{}{"care_plan": models.CarePlan{}})
	req := httptest.NewRequest(http.MethodPost, "/api/plants", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleListPlants_Empty(t *testing.T) {
	_, mux := newTestHandlerWithStore(nil, &mockStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/plants", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var entries []store.PlantEntry
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if entries == nil {
		t.Error("expected empty array [], got null")
	}
}

func TestHandleGetPlant_Found(t *testing.T) {
	ms := &mockStore{
		entries: []store.PlantEntry{
			{ID: "abc123", CreatedAt: time.Now(), CarePlan: models.CarePlan{PlantName: "Fern"}},
		},
	}
	_, mux := newTestHandlerWithStore(nil, ms)

	req := httptest.NewRequest(http.MethodGet, "/api/plants/abc123", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var entry store.PlantEntry
	if err := json.NewDecoder(w.Body).Decode(&entry); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if entry.ID != "abc123" {
		t.Errorf("expected ID abc123, got %q", entry.ID)
	}
}

func TestHandleGetPlant_NotFound(t *testing.T) {
	_, mux := newTestHandlerWithStore(nil, &mockStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/plants/missing", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleDeletePlant(t *testing.T) {
	ms := &mockStore{
		entries: []store.PlantEntry{
			{ID: "del-me", CreatedAt: time.Now(), CarePlan: models.CarePlan{PlantName: "Cactus"}},
		},
	}
	_, mux := newTestHandlerWithStore(nil, ms)

	req := httptest.NewRequest(http.MethodDelete, "/api/plants/del-me", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
	if len(ms.entries) != 0 {
		t.Errorf("expected entry to be deleted, %d remain", len(ms.entries))
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
