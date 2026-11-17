// Package snspub publishes events to an SNS topic. It implements ingest.Publisher.
package snspub

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"

	"github.com/JoshPaul7/rewardstream/internal/event"
)

type Publisher struct {
	client   *sns.Client
	topicARN string
}

// New builds the SNS client from the shared, identity-checked config.
func New(cfg aws.Config, topicARN string) *Publisher {
	return &Publisher{client: sns.NewFromConfig(cfg), topicARN: topicARN}
}

func (p *Publisher) Publish(ctx context.Context, e event.Event) error {
	body, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event %s: %w", e.ID, err)
	}
	_, err = p.client.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(p.topicARN),
		Message:  aws.String(string(body)),
		// Lets future subscribers filter by type (SNS subscription filter policies).
		MessageAttributes: map[string]types.MessageAttributeValue{
			"event_type": {DataType: aws.String("String"), StringValue: aws.String(string(e.Type))},
		},
	})
	if err != nil {
		return fmt.Errorf("sns publish %s: %w", e.ID, err)
	}
	return nil
}
