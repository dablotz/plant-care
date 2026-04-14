package store

import (
	"context"
	"time"

	"github.com/dablotz/plantcare/internal/models"
)

// PlantEntry is a saved plant with its full care plan.
type PlantEntry struct {
	ID        string          `json:"id"`
	CreatedAt time.Time       `json:"created_at"`
	CarePlan  models.CarePlan `json:"care_plan"`
}

// PlantStore is the interface for plant library persistence backends.
type PlantStore interface {
	SavePlant(ctx context.Context, entry PlantEntry) error
	ListPlants(ctx context.Context) ([]PlantEntry, error)
	GetPlant(ctx context.Context, id string) (*PlantEntry, error)
	DeletePlant(ctx context.Context, id string) error
}

// AppSettings holds the active AI backend selection and encrypted credentials.
type AppSettings struct {
	ActiveBackend string // "anthropic" | "gemini" | "ollama" | ""
	AnthropicKey  string // encrypted at rest, empty if unset
	GeminiKey     string // encrypted at rest, empty if unset
	OllamaBaseURL string // plaintext URL
	OllamaModel   string // e.g. "llava"
}

// SettingsStore is the interface for persisting app configuration.
type SettingsStore interface {
	GetSettings(ctx context.Context) (*AppSettings, error)
	SaveSettings(ctx context.Context, s AppSettings) error
}
