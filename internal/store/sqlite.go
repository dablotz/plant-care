package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// SQLiteStore is a PlantStore backed by a local SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the SQLite database at path and ensures
// the schema is up to date. Use ":memory:" for in-process testing.
func NewSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite db: %w", err)
	}

	// WAL mode allows concurrent readers while a writer is active.
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	const schema = `
CREATE TABLE IF NOT EXISTS plants (
    id         TEXT PRIMARY KEY,
    created_at TEXT NOT NULL,
    care_plan  TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    active_backend  TEXT NOT NULL DEFAULT '',
    anthropic_key   TEXT NOT NULL DEFAULT '',
    gemini_key      TEXT NOT NULL DEFAULT '',
    ollama_base_url TEXT NOT NULL DEFAULT '',
    ollama_model    TEXT NOT NULL DEFAULT 'llava'
)`
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Close releases the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) SavePlant(ctx context.Context, entry PlantEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	planJSON, err := json.Marshal(entry.CarePlan)
	if err != nil {
		return fmt.Errorf("marshaling care plan: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO plants (id, created_at, care_plan) VALUES (?, ?, ?)`,
		entry.ID,
		entry.CreatedAt.UTC().Format(time.RFC3339),
		string(planJSON),
	)
	return err
}

func (s *SQLiteStore) ListPlants(ctx context.Context) ([]PlantEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, created_at, care_plan FROM plants ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	var entries []PlantEntry
	for rows.Next() {
		e, err := scanEntry(rows.Scan)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *SQLiteStore) GetPlant(ctx context.Context, id string) (*PlantEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, created_at, care_plan FROM plants WHERE id = ?`, id)
	e, err := scanEntry(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *SQLiteStore) DeletePlant(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM plants WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) GetSettings(ctx context.Context) (*AppSettings, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT active_backend, anthropic_key, gemini_key, ollama_base_url, ollama_model
		 FROM settings WHERE id = 1`)
	var settings AppSettings
	err := row.Scan(
		&settings.ActiveBackend,
		&settings.AnthropicKey,
		&settings.GeminiKey,
		&settings.OllamaBaseURL,
		&settings.OllamaModel,
	)
	if err == sql.ErrNoRows {
		return &AppSettings{OllamaModel: "llava"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading settings: %w", err)
	}
	return &settings, nil
}

func (s *SQLiteStore) SaveSettings(ctx context.Context, settings AppSettings) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO settings
		 (id, active_backend, anthropic_key, gemini_key, ollama_base_url, ollama_model)
		 VALUES (1, ?, ?, ?, ?, ?)`,
		settings.ActiveBackend,
		settings.AnthropicKey,
		settings.GeminiKey,
		settings.OllamaBaseURL,
		settings.OllamaModel,
	)
	if err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	return nil
}

// scanEntry reads a row using the provided scan function.
func scanEntry(scan func(...any) error) (PlantEntry, error) {
	var (
		id         string
		createdStr string
		planJSON   string
	)
	if err := scan(&id, &createdStr, &planJSON); err != nil {
		return PlantEntry{}, err
	}

	createdAt, err := time.Parse(time.RFC3339, createdStr)
	if err != nil {
		return PlantEntry{}, fmt.Errorf("parsing created_at: %w", err)
	}

	var entry PlantEntry
	if err := json.Unmarshal([]byte(planJSON), &entry.CarePlan); err != nil {
		return PlantEntry{}, fmt.Errorf("unmarshaling care plan: %w", err)
	}
	entry.ID = id
	entry.CreatedAt = createdAt
	return entry, nil
}
