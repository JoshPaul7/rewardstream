// Package processor consumes events from SQS, writes raw events to the lake,
// and records counters idempotently.
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

// Lake stores raw events (at-least-once).
type Lake interface {
	WriteBatch(ctx context.Context, events []event.Event) error
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
	poison                // can never succeed: release now so redrive moves it to the DLQ
	retry                 // transient failure: leave it; the visibility timeout is the backoff
)

// batchTimeout must stay below the queue's VisibilityTimeout (30 s).
const batchTimeout = 25 * time.Second

type Processor struct {
	Queue    Queue
	QueueURL string
	Store    Store
	Lake     Lake // optional: nil disables the raw-event lake
	Log      *slog.Logger
	Now      func() time.Time
}

// parse turns a message body into a valid event; ok=false means poison.
func (p *Processor) parse(body string) (event.Event, bool) {
	var e event.Event
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		p.Log.Warn("poison: malformed JSON", "err", err)
		return e, false
	}
	// Validate again: the queue may receive messages that didn't come through ingest-api.
	if err := e.Validate(p.Now()); err != nil {
		p.Log.Warn("poison: invalid event", "event_id", e.ID, "err", err)
		return e, false
	}
	return e, true
}

// record stores one event's counters exactly once.
func (p *Processor) record(ctx context.Context, e event.Event) outcome {
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

// processBatch returns one outcome per body. Raw events go to the lake BEFORE
// counters and deletes: if the lake write fails, nothing is acknowledged.
func (p *Processor) processBatch(ctx context.Context, bodies []string) []outcome {
	out := make([]outcome, len(bodies))
	var valid []event.Event
	var idx []int
	for i, b := range bodies {
		e, ok := p.parse(b)
		if !ok {
			out[i] = poison
			continue
		}
		valid = append(valid, e)
		idx = append(idx, i)
	}
	if len(valid) == 0 {
		return out
	}

	if p.Lake != nil {
		if err := p.Lake.WriteBatch(ctx, valid); err != nil {
			p.Log.Error("lake write failed, will retry batch", "events", len(valid), "err", err)
			for _, i := range idx {
				out[i] = retry
			}
			return out
		}
	}
	for j, e := range valid {
		out[idx[j]] = p.record(ctx, e)
	}
	return out
}

// Run long-polls until ctx is cancelled. A batch already received is finished first.
func (p *Processor) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		out, err := p.Queue.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(p.QueueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			p.Log.Error("receive failed", "err", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
			}
			continue
		}
		if len(out.Messages) == 0 {
			continue
		}

		bodies := make([]string, len(out.Messages))
		for i, m := range out.Messages {
			bodies[i] = aws.ToString(m.Body)
		}
		batchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), batchTimeout)
		outcomes := p.processBatch(batchCtx, bodies)
		for i, m := range out.Messages {
			p.settle(batchCtx, m.ReceiptHandle, outcomes[i])
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
			p.Log.Error("delete failed", "err", err) // it comes back; the store reports a duplicate
		}
	case poison:
		if _, err := p.Queue.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
			QueueUrl: aws.String(p.QueueURL), ReceiptHandle: receipt, VisibilityTimeout: 0,
		}); err != nil {
			p.Log.Error("release failed", "err", err)
		}
	case retry:
		// Nothing: it reappears after the visibility timeout.
	}
}
