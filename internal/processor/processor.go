// Package processor consumes events from SQS and records them idempotently.
package processor

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/JoshPaul7/rewardstream/internal/event"
)

// Store records an event exactly once; duplicate=true means it was seen before.
type Store interface {
	Record(ctx context.Context, e event.Event) (duplicate bool, err error)
}

// Queue is the subset of the SQS client the processor uses.
type Queue interface {
	ReceiveMessage(ctx context.Context, in *sqs.ReceiveMessageInput, opts ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, in *sqs.DeleteMessageInput, opts ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(ctx context.Context, in *sqs.ChangeMessageVisibilityInput, opts ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error)
}

type outcome int

const (
	ack    outcome = iota // processed or duplicate: delete it
	poison                // can never succeed: release it now so redrive moves it to the DLQ
	retry                 // transient failure: leave it; the visibility timeout is the backoff
)

// batchTimeout must stay below the queue's VisibilityTimeout (30 s), or a message
// becomes visible to another consumer while we are still working on it.
const batchTimeout = 25 * time.Second

type Processor struct {
	Queue    Queue
	QueueURL string
	Store    Store
	Log      *slog.Logger
	Now      func() time.Time
}

// handle decides what to do with one message body. It is the unit under test.
func (p *Processor) handle(ctx context.Context, body string) outcome {
	var e event.Event
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		p.Log.Warn("poison: malformed JSON", "err", err)
		return poison
	}
	// Validate again: the queue may receive messages that didn't come through ingest-api.
	if err := e.Validate(p.Now()); err != nil {
		p.Log.Warn("poison: invalid event", "event_id", e.ID, "err", err)
		return poison
	}
	dup, err := p.Store.Record(ctx, e)
	if err != nil {
		p.Log.Error("store failed, will retry", "event_id", e.ID, "err", err)
		return retry
	}
	if dup {
		p.Log.Info("duplicate dropped", "event_id", e.ID)
	} else {
		p.Log.Info("recorded", "event_id", e.ID, "type", e.Type, "campaign_id", e.CampaignID)
	}
	return ack
}

// Run long-polls until ctx is cancelled. A batch already received is finished first.
func (p *Processor) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		out, err := p.Queue.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(p.QueueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20, // long poll: wait for messages instead of hammering an empty queue
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil // shutting down
			}
			p.Log.Error("receive failed", "err", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
			continue
		}

		batchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), batchTimeout)
		for _, m := range out.Messages {
			p.settle(batchCtx, m.ReceiptHandle, p.handle(batchCtx, aws.ToString(m.Body)))
		}
		cancel()
	}
	return nil
}

func (p *Processor) settle(ctx context.Context, receipt *string, o outcome) {
	switch o {
	case ack:
		if _, err := p.Queue.DeleteMessage(ctx, &sqs.DeleteMessageInput{
			QueueUrl: aws.String(p.QueueURL), ReceiptHandle: receipt,
		}); err != nil {
			// Not fatal: the message comes back and the store reports a duplicate.
			p.Log.Error("delete failed", "err", err)
		}
	case poison:
		// Visibility 0: receivable again immediately, so it reaches maxReceiveCount
		// (and the DLQ) in seconds instead of after three 30 s timeouts.
		if _, err := p.Queue.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
			QueueUrl: aws.String(p.QueueURL), ReceiptHandle: receipt, VisibilityTimeout: 0,
		}); err != nil {
			p.Log.Error("release failed", "err", err)
		}
	case retry:
		// Nothing: it reappears after the visibility timeout.
	}
}
