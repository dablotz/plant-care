package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
)

// dynamoItem mirrors PlantEntry for DynamoDB attribute marshaling.
type dynamoItem struct {
	ID        string `dynamodbav:"id"`
	CreatedAt string `dynamodbav:"created_at"` // RFC3339 string
	CarePlan  string `dynamodbav:"care_plan"`  // JSON blob
}

// DynamoStore is a PlantStore backed by AWS DynamoDB.
type DynamoStore struct {
	client    *dynamodb.Client
	tableName string
}

// NewDynamoStore creates a DynamoStore using the provided AWS config.
func NewDynamoStore(_ context.Context, cfg aws.Config, tableName string) *DynamoStore {
	return &DynamoStore{
		client:    dynamodb.NewFromConfig(cfg),
		tableName: tableName,
	}
}

func (d *DynamoStore) SavePlant(ctx context.Context, entry PlantEntry) error {
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

	item := dynamoItem{
		ID:        entry.ID,
		CreatedAt: entry.CreatedAt.UTC().Format(time.RFC3339),
		CarePlan:  string(planJSON),
	}

	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return fmt.Errorf("marshaling DynamoDB item: %w", err)
	}

	_, err = d.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item:      av,
	})
	return err
}

func (d *DynamoStore) ListPlants(ctx context.Context) ([]PlantEntry, error) {
	out, err := d.client.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(d.tableName),
		Limit:     aws.Int32(100),
	})
	if err != nil {
		return nil, fmt.Errorf("scanning DynamoDB table: %w", err)
	}

	entries := make([]PlantEntry, 0, len(out.Items))
	for _, av := range out.Items {
		e, err := unmarshalItem(av)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (d *DynamoStore) GetPlant(ctx context.Context, id string) (*PlantEntry, error) {
	out, err := d.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("getting DynamoDB item: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}

	e, err := unmarshalItem(out.Item)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (d *DynamoStore) DeletePlant(ctx context.Context, id string) error {
	_, err := d.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	return err
}

func unmarshalItem(av map[string]types.AttributeValue) (PlantEntry, error) {
	var item dynamoItem
	if err := attributevalue.UnmarshalMap(av, &item); err != nil {
		return PlantEntry{}, fmt.Errorf("unmarshaling DynamoDB item: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339, item.CreatedAt)
	if err != nil {
		return PlantEntry{}, fmt.Errorf("parsing created_at: %w", err)
	}

	var entry PlantEntry
	if err := json.Unmarshal([]byte(item.CarePlan), &entry.CarePlan); err != nil {
		return PlantEntry{}, fmt.Errorf("unmarshaling care plan: %w", err)
	}
	entry.ID = item.ID
	entry.CreatedAt = createdAt
	return entry, nil
}
