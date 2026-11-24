// Package s3sink writes raw events to the S3 data lake as JSON Lines,
// one object per (event_type, dt, hour) partition per batch.
package s3sink

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/JoshPaul7/rewardstream/internal/event"
)

type Sink struct {
	client *s3.Client
	bucket string
}

// New builds the S3 client from the shared, identity-checked config.
// pathStyle is for emulators (http://host:4566/bucket/key); real AWS uses
// virtual-hosted style (https://bucket.s3.region.amazonaws.com/key).
func New(cfg aws.Config, bucket string, pathStyle bool) *Sink {
	client := s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = pathStyle })
	return &Sink{client: client, bucket: bucket}
}

// WriteBatch groups events by partition and writes one object per group.
// A retry after partial failure may rewrite a group: the lake is at-least-once.
func (s *Sink) WriteBatch(ctx context.Context, events []event.Event) error {
	groups := map[string]*bytes.Buffer{}
	for _, e := range events {
		t := e.OccurredAt.UTC()
		prefix := fmt.Sprintf("raw/event_type=%s/dt=%s/hour=%02d/", e.Type, t.Format("2006-01-02"), t.Hour())
		buf, ok := groups[prefix]
		if !ok {
			buf = &bytes.Buffer{}
			groups[prefix] = buf
		}
		line, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("marshal event %s: %w", e.ID, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	for prefix, buf := range groups {
		key := prefix + time.Now().UTC().Format("20060102T150405Z") + "-" + rand.Text() + ".jsonl"
		if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(s.bucket),
			Key:         aws.String(key),
			Body:        bytes.NewReader(buf.Bytes()),
			ContentType: aws.String("application/x-ndjson"),
		}); err != nil {
			return fmt.Errorf("put s3://%s/%s: %w", s.bucket, key, err)
		}
	}
	return nil
}
