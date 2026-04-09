package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/dablotz/plantcare/internal/models"
)

// ModelID is the Anthropic model used for plant identification.
// Must be a model that supports vision (image input).
const ModelID = "claude-haiku-4-5-20251001"

// Client wraps the Anthropic Messages API.
type Client struct {
	inner sdk.Client
}

// New creates an Anthropic client. If apiKey is empty the SDK reads
// ANTHROPIC_API_KEY from the environment.
func New(apiKey string) *Client {
	opts := []option.RequestOption{}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	return &Client{inner: sdk.NewClient(opts...)}
}

// IdentifyAndPlan identifies a plant (by name or image) and returns a structured CarePlan.
func (c *Client) IdentifyAndPlan(ctx context.Context, req models.PlantIdentifyRequest) (*models.CarePlan, error) {
	systemPrompt := `You are an expert botanist and houseplant care specialist.
Your job is to identify houseplants and produce detailed, actionable care plans.
You MUST respond with valid JSON only — no markdown fences, no prose outside the JSON.
The JSON must conform exactly to this schema:
{
  "plant_name": "string",
  "common_name": "string",
  "scientific_name": "string",
  "summary": "string (2-3 sentences)",
  "light": "string",
  "humidity": "string",
  "temperature": "string",
  "soil_type": "string",
  "toxicity_note": "string or empty",
  "pro_tips": ["string", ...],
  "schedule": [
    {
      "task": "string",
      "frequency_days": integer,
      "notes": "string or empty",
      "seasonal_note": "string or empty"
    }
  ]
}
Schedule must include at minimum: Watering, Fertilizing, and Repotting tasks.
Include Misting if the plant benefits from it. Include Pruning if applicable.
frequency_days must be a positive integer (e.g. water every 7 days = 7).`

	var parts []sdk.ContentBlockParamUnion

	if req.ImageBase64 != "" {
		mime := req.ImageMIME
		if mime == "" {
			mime = "image/jpeg"
		}
		parts = append(parts, sdk.NewImageBlockBase64(mime, req.ImageBase64))
		parts = append(parts, sdk.NewTextBlock("Identify this houseplant and produce a full care plan as JSON."))
	} else if req.Name != "" {
		parts = append(parts, sdk.NewTextBlock(
			fmt.Sprintf("Produce a full care plan for the houseplant: %q. Respond with JSON only.", req.Name),
		))
	} else {
		return nil, fmt.Errorf("request must include either a plant name or an image")
	}

	msg, err := c.inner.Messages.New(ctx, sdk.MessageNewParams{
		Model:     sdk.Model(ModelID),
		MaxTokens: 2048,
		System: []sdk.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []sdk.MessageParam{
			sdk.NewUserMessage(parts...),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("calling Anthropic API: %w", err)
	}

	var rawJSON string
	for _, block := range msg.Content {
		if block.Type == "text" {
			rawJSON = strings.TrimSpace(block.Text)
			break
		}
	}
	if rawJSON == "" {
		return nil, fmt.Errorf("no text content in Anthropic response")
	}

	// Strip any accidental markdown fences
	rawJSON = strings.TrimPrefix(rawJSON, "```json")
	rawJSON = strings.TrimPrefix(rawJSON, "```")
	rawJSON = strings.TrimSuffix(rawJSON, "```")
	rawJSON = strings.TrimSpace(rawJSON)

	var plan models.CarePlan
	if err := json.Unmarshal([]byte(rawJSON), &plan); err != nil {
		return nil, fmt.Errorf("parsing care plan JSON from model: %w\nraw response: %s", err, rawJSON)
	}

	return &plan, nil
}
