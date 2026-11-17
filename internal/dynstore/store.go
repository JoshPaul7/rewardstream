// Package dynstore records events in DynamoDB exactly once per event_id.
package dynstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/JoshPaul7/rewardstream/internal/event"
)

// DedupWindow must exceed how long a message can still be redelivered.
const DedupWindow = 7 * 24 * time.Hour

type Store struct {
	client *dynamodb.Client
	table  string
}

// New builds the DynamoDB client from the shared, identity-checked config.
func New(cfg aws.Config, table string) *Store {
	return &Store{client: dynamodb.NewFromConfig(cfg), table: table}
}

// Record writes a dedup marker and bumps the campaign's daily counter in ONE
// transaction: both happen or neither does. duplicate=true means event_id was seen before.
func (s *Store) Record(ctx context.Context, e event.Event) (duplicate bool, err error) {
	day := e.OccurredAt.UTC().Format("2006-01-02")
	expires := strconv.FormatInt(time.Now().Add(DedupWindow).Unix(), 10)

	update := "ADD #c :one"
	names := map[string]string{"#c": "count_" + string(e.Type)}
	values := map[string]types.AttributeValue{":one": &types.AttributeValueMemberN{Value: "1"}}
	if e.Type == event.TypeRewardGranted {
		update += ", reward_total :amt"
		values[":amt"] = &types.AttributeValueMemberN{Value: strconv.FormatInt(e.RewardAmount, 10)}
	}

	_, err = s.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []types.TransactWriteItem{
			{Put: &types.Put{ // item 0: dedup marker, only if absent
				TableName: aws.String(s.table),
				Item: map[string]types.AttributeValue{
					"pk":         &types.AttributeValueMemberS{Value: "EVENT#" + e.ID},
					"sk":         &types.AttributeValueMemberS{Value: "EVENT"},
					"expires_at": &types.AttributeValueMemberN{Value: expires},
				},
				ConditionExpression: aws.String("attribute_not_exists(pk)"),
			}},
			{Update: &types.Update{ // item 1: atomic counter
				TableName: aws.String(s.table),
				Key: map[string]types.AttributeValue{
					"pk": &types.AttributeValueMemberS{Value: "CAMPAIGN#" + e.CampaignID},
					"sk": &types.AttributeValueMemberS{Value: "DAY#" + day},
				},
				UpdateExpression:          aws.String(update),
				ExpressionAttributeNames:  names,
				ExpressionAttributeValues: values,
			}},
		},
	})
	if err == nil {
		return false, nil
	}

	var tce *types.TransactionCanceledException
	if errors.As(err, &tce) && len(tce.CancellationReasons) > 0 &&
		aws.ToString(tce.CancellationReasons[0].Code) == "ConditionalCheckFailed" {
		return true, nil // the dedup marker already exists
	}
	return false, fmt.Errorf("record event %s: %w", e.ID, err)
}
