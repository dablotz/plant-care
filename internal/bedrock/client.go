package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/dablotz/plantcare/internal/models"
)

// ModelID is the Bedrock model used. Claude 3 Sonnet supports vision.
// Swap for "anthropic.claude-3-haiku-20240307-v1:0" for faster/cheaper calls.
const ModelID = "anthropic.claude-3-sonnet-20240229-v1:0"

// Client wraps the Bedrock runtime client.
type Client struct {
	runtime *bedrockruntime.Client
	region  string
}

// New creates a Bedrock client using the default AWS credential chain
// (env vars, ~/.aws/credentials, EC2 instance role, etc.)
func New(ctx context.Context, region string) (*Client, error) {
	if region == "" {
		region = "us-east-1"
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return &Client{
		runtime: bedrockruntime.NewFromConfig(cfg),
		region:  region,
	}, nil
}

// --- Bedrock request/response types for Claude Messages API ---

type contentBlock struct {
	Type   string      `json:"type"`
	Text   string      `json:"text,omitempty"`
	Source *imageSource `json:"source,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/jpeg" etc.
	Data      string `json:"data"`       // base64-encoded bytes
}

type message struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type invokeRequest struct {
	AnthropicVersion string    `json:"anthropic_version"`
	MaxTokens        int       `json:"max_tokens"`
	System           string    `json:"system"`
	Messages         []message `json:"messages"`
}

type invokeResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
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

	// Build the user message content blocks
	var content []contentBlock

	if req.ImageBase64 != "" {
		mime := req.ImageMIME
		if mime == "" {
			mime = "image/jpeg"
		}
		content = append(content, contentBlock{
			Type: "image",
			Source: &imageSource{
				Type:      "base64",
				MediaType: mime,
				Data:      req.ImageBase64,
			},
		})
		content = append(content, contentBlock{
			Type: "text",
			Text: "Identify this houseplant and produce a full care plan as JSON.",
		})
	} else if req.Name != "" {
		content = append(content, contentBlock{
			Type: "text",
			Text: fmt.Sprintf("Produce a full care plan for the houseplant: %q. Respond with JSON only.", req.Name),
		})
	} else {
		return nil, fmt.Errorf("request must include either a plant name or an image")
	}

	body := invokeRequest{
		AnthropicVersion: "bedrock-2023-05-31",
		MaxTokens:        2048,
		System:           systemPrompt,
		Messages: []message{
			{Role: "user", Content: content},
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	resp, err := c.runtime.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(ModelID),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        bodyBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("invoking Bedrock model: %w", err)
	}

	var invokeResp invokeResponse
	if err := json.Unmarshal(resp.Body, &invokeResp); err != nil {
		return nil, fmt.Errorf("unmarshaling Bedrock response: %w", err)
	}

	// Extract the text content block
	var rawJSON string
	for _, block := range invokeResp.Content {
		if block.Type == "text" {
			rawJSON = strings.TrimSpace(block.Text)
			break
		}
	}
	if rawJSON == "" {
		return nil, fmt.Errorf("no text content in Bedrock response")
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
