// Command event-processor consumes events from SQS and records them in DynamoDB.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/JoshPaul7/rewardstream/internal/awsenv"
	"github.com/JoshPaul7/rewardstream/internal/dynstore"
	"github.com/JoshPaul7/rewardstream/internal/processor"
	"github.com/JoshPaul7/rewardstream/internal/s3sink"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("event-processor stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	mode := awsenv.Mode(os.Getenv("RS_MODE"))
	queueURL, table := os.Getenv("RS_QUEUE_URL"), os.Getenv("RS_TABLE")
	if queueURL == "" || table == "" {
		return errors.New("RS_QUEUE_URL and RS_TABLE are required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	cfg, err := awsenv.Load(startCtx, mode, os.Getenv("RS_EXPECTED_ACCOUNT_ID"))
	cancel()
	if err != nil {
		return err
	}

	// A nil interface, not a nil *s3sink.Sink: see explain-back 2.
	var lake processor.Lake
	if bucket := os.Getenv("RS_LAKE_BUCKET"); bucket != "" {
		lake = s3sink.New(cfg, bucket, mode == awsenv.ModeLocal)
	}

	p := &processor.Processor{
		Queue: sqs.NewFromConfig(cfg), QueueURL: queueURL,
		Store: dynstore.New(cfg, table), Lake: lake, Log: log, Now: time.Now,
	}
	log.Info("consuming", "queue", queueURL, "mode", mode, "lake", os.Getenv("RS_LAKE_BUCKET"))
	err = p.Run(ctx)
	log.Info("stopped")
	return err
}
