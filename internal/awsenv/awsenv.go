// Package awsenv loads the single shared AWS config and refuses to return it
// unless the caller's identity matches the expected account (fail closed).
package awsenv

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Mode is where the service is allowed to run.
type Mode string

const (
	ModeLocal Mode = "local" // Floci / emulator
	ModeAWS   Mode = "aws"   // real AWS account
)

// LocalAccountID is the account ID Floci reports for any credentials.
const LocalAccountID = "000000000000"

// Load returns the aws.Config that EVERY client in the service must be built from,
// so the identity check below covers all of them.
func Load(ctx context.Context, mode Mode, wantAccount string) (aws.Config, error) {
	switch mode {
	case ModeLocal:
		if os.Getenv("AWS_ENDPOINT_URL") == "" {
			return aws.Config{}, errors.New("mode=local but AWS_ENDPOINT_URL is not set (source deploy/local/env.sh)")
		}
		if wantAccount == "" {
			wantAccount = LocalAccountID
		}
	case ModeAWS:
		if wantAccount == "" {
			return aws.Config{}, errors.New("mode=aws requires RS_EXPECTED_ACCOUNT_ID")
		}
	default:
		return aws.Config{}, fmt.Errorf("unknown RS_MODE %q (want local or aws)", mode)
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return aws.Config{}, fmt.Errorf("load aws config: %w", err)
	}

	out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		// Fail closed: "could not verify" is treated exactly like "verified wrong".
		return aws.Config{}, fmt.Errorf("identity check failed, refusing to start: %w", err)
	}
	if got := aws.ToString(out.Account); got != wantAccount {
		return aws.Config{}, fmt.Errorf("identity check: account %s, want %s for mode=%s; refusing to start",
			got, wantAccount, mode)
	}
	return cfg, nil
}
