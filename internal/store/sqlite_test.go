package store

import (
	"context"
	"testing"
	"time"

	"github.com/dablotz/plantcare/internal/models"
)

func openTestDB(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func testEntry() PlantEntry {
	return PlantEntry{
		ID:        "test-id-1",
		CreatedAt: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		CarePlan: models.CarePlan{
			PlantName:  "Monstera",
			CommonName: "Swiss Cheese Plant",
			Summary:    "A tropical plant.",
			Schedule: []models.CareScheduleItem{
				{Task: "Watering", FrequencyDays: 7},
			},
		},
	}
}

func TestSQLiteStore_SaveAndGet(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()
	entry := testEntry()

	if err := s.SavePlant(ctx, entry); err != nil {
		t.Fatalf("SavePlant: %v", err)
	}

	got, err := s.GetPlant(ctx, entry.ID)
	if err != nil {
		t.Fatalf("GetPlant: %v", err)
	}
	if got == nil {
		t.Fatal("GetPlant returned nil for existing entry")
	}
	if got.ID != entry.ID {
		t.Errorf("ID: got %q, want %q", got.ID, entry.ID)
	}
	if got.CarePlan.PlantName != entry.CarePlan.PlantName {
		t.Errorf("PlantName: got %q, want %q", got.CarePlan.PlantName, entry.CarePlan.PlantName)
	}
	if len(got.CarePlan.Schedule) != 1 {
		t.Errorf("Schedule len: got %d, want 1", len(got.CarePlan.Schedule))
	}
}

func TestSQLiteStore_GetMissing(t *testing.T) {
	s := openTestDB(t)

	got, err := s.GetPlant(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("GetPlant returned unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for missing entry, got a value")
	}
}

func TestSQLiteStore_List(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	entries, err := s.ListPlants(ctx)
	if err != nil {
		t.Fatalf("ListPlants on empty DB: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty list, got %d entries", len(entries))
	}

	e1 := testEntry()
	e1.ID = "id-1"
	e1.CreatedAt = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	e2 := testEntry()
	e2.ID = "id-2"
	e2.CarePlan.PlantName = "Pothos"
	e2.CreatedAt = time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC) // newer

	if err := s.SavePlant(ctx, e1); err != nil {
		t.Fatalf("SavePlant e1: %v", err)
	}
	if err := s.SavePlant(ctx, e2); err != nil {
		t.Fatalf("SavePlant e2: %v", err)
	}

	entries, err = s.ListPlants(ctx)
	if err != nil {
		t.Fatalf("ListPlants: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Newest first
	if entries[0].ID != "id-2" {
		t.Errorf("expected newest first, got ID %q", entries[0].ID)
	}
}

func TestSQLiteStore_Delete(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()
	entry := testEntry()

	if err := s.SavePlant(ctx, entry); err != nil {
		t.Fatalf("SavePlant: %v", err)
	}

	if err := s.DeletePlant(ctx, entry.ID); err != nil {
		t.Fatalf("DeletePlant: %v", err)
	}

	got, err := s.GetPlant(ctx, entry.ID)
	if err != nil {
		t.Fatalf("GetPlant after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete, got entry")
	}
}

func TestSQLiteStore_DeleteIdempotent(t *testing.T) {
	s := openTestDB(t)

	// Deleting non-existent ID should not error
	if err := s.DeletePlant(context.Background(), "ghost-id"); err != nil {
		t.Errorf("DeletePlant non-existent: expected nil error, got %v", err)
	}
}

func TestSQLiteStore_GeneratesIDIfEmpty(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	entry := testEntry()
	entry.ID = "" // let SavePlant generate it

	if err := s.SavePlant(ctx, entry); err != nil {
		t.Fatalf("SavePlant: %v", err)
	}

	entries, _ := s.ListPlants(ctx)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID == "" {
		t.Error("expected a generated ID, got empty string")
	}
}

// --- settings ---

func TestSQLiteStore_GetSettings_Defaults(t *testing.T) {
	s := openTestDB(t)

	got, err := s.GetSettings(context.Background())
	if err != nil {
		t.Fatalf("GetSettings on empty DB: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil AppSettings, got nil")
	}
	if got.ActiveBackend != "" {
		t.Errorf("expected empty ActiveBackend, got %q", got.ActiveBackend)
	}
	if got.OllamaModel != "llava" {
		t.Errorf("expected default OllamaModel 'llava', got %q", got.OllamaModel)
	}
}

func TestSQLiteStore_SaveAndGetSettings(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	in := AppSettings{
		ActiveBackend: "gemini",
		GeminiKey:     "encrypted-key-value",
		OllamaModel:   "llava",
	}
	if err := s.SaveSettings(ctx, in); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	got, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got.ActiveBackend != "gemini" {
		t.Errorf("ActiveBackend: got %q, want %q", got.ActiveBackend, "gemini")
	}
	if got.GeminiKey != "encrypted-key-value" {
		t.Errorf("GeminiKey: got %q, want %q", got.GeminiKey, "encrypted-key-value")
	}
}

func TestSQLiteStore_SaveSettings_Upsert(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	first := AppSettings{ActiveBackend: "anthropic", AnthropicKey: "key1"}
	if err := s.SaveSettings(ctx, first); err != nil {
		t.Fatalf("first SaveSettings: %v", err)
	}

	second := AppSettings{ActiveBackend: "gemini", GeminiKey: "key2"}
	if err := s.SaveSettings(ctx, second); err != nil {
		t.Fatalf("second SaveSettings: %v", err)
	}

	got, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if got.ActiveBackend != "gemini" {
		t.Errorf("expected upserted ActiveBackend 'gemini', got %q", got.ActiveBackend)
	}
	if got.AnthropicKey != "" {
		t.Errorf("expected AnthropicKey cleared by upsert, got %q", got.AnthropicKey)
	}
}
