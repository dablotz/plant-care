package gbif

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	suggestURL = "https://api.gbif.org/v1/species/suggest"
	searchURL  = "https://api.gbif.org/v1/species/search"
)

// Match is a result from the GBIF API.
type Match struct {
	CanonicalName string `json:"canonicalName"`
	Rank          string `json:"rank"`
	Status        string `json:"status"`
	Confidence    int    `json:"confidence"` // only populated from suggest
}

// Client queries the GBIF species API.
type Client struct {
	http *http.Client
}

// New returns a GBIF client with a conservative timeout.
func New() *Client {
	return &Client{http: &http.Client{Timeout: 5 * time.Second}}
}

// Lookup searches for a plant by name and returns the best match, or nil if
// nothing in the plant kingdom matches. It tries the suggest endpoint first
// (fast, scientific names) and falls back to the full-text search endpoint
// (slower, also covers vernacular/common names). If the API is unreachable,
// an error is returned so the caller can decide whether to fail open or closed.
func (c *Client) Lookup(ctx context.Context, name string) (*Match, error) {
	match, err := c.suggest(ctx, name)
	if err != nil {
		return nil, err
	}
	if match != nil {
		return match, nil
	}
	return c.search(ctx, name)
}

// suggest calls the species/suggest endpoint which matches on scientific names.
func (c *Client) suggest(ctx context.Context, name string) (*Match, error) {
	u := suggestURL + "?kingdom=Plantae&limit=5&q=" + url.QueryEscape(name)
	var results []Match
	if err := c.get(ctx, u, &results); err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return &results[0], nil
}

// search calls the species/search endpoint which also covers vernacular/common names.
func (c *Client) search(ctx context.Context, name string) (*Match, error) {
	u := searchURL + "?kingdom=Plantae&limit=5&q=" + url.QueryEscape(name)
	var page struct {
		Results []Match `json:"results"`
	}
	if err := c.get(ctx, u, &page); err != nil {
		return nil, err
	}
	if len(page.Results) == 0 {
		return nil, nil
	}
	return &page.Results[0], nil
}

func (c *Client) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("building GBIF request: %w", err)
	}
	req.Header.Set("User-Agent", "plantcare-app/1.0")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling GBIF API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GBIF API returned %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("parsing GBIF response: %w", err)
	}
	return nil
}
