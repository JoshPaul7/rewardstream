#!/usr/bin/env bash
# Creates Tier 0 messaging resources on Floci. Safe to re-run.
# Phase 3 replaces this with the Terraform `messaging` module.
set -euo pipefail

: "${AWS_ENDPOINT_URL:?source deploy/local/env.sh first}"
command -v jq >/dev/null || { echo "jq is required (sudo apt install jq)" >&2; exit 1; }

# Same guard as the services: never run this against a real account.
acct=$(aws sts get-caller-identity --query Account --output text)
if [ "$acct" != "000000000000" ]; then
  echo "refusing: account $acct is not Floci (000000000000)" >&2
  exit 1
fi

topic_arn=$(aws sns create-topic --name rewardstream-events --query TopicArn --output text)

dlq_url=$(aws sqs create-queue --queue-name rewardstream-processing-dlq --query QueueUrl --output text)
dlq_arn=$(aws sqs get-queue-attributes --queue-url "$dlq_url" --attribute-names QueueArn \
  --query Attributes.QueueArn --output text)

# Created without attributes, then set separately: create-queue with changed
# attributes fails on re-run, set-queue-attributes doesn't.
q_url=$(aws sqs create-queue --queue-name rewardstream-processing --query QueueUrl --output text)
q_arn=$(aws sqs get-queue-attributes --queue-url "$q_url" --attribute-names QueueArn \
  --query Attributes.QueueArn --output text)

# Poison messages move to the DLQ after 3 failed receives.
attrs=$(jq -nc --arg dlq "$dlq_arn" \
  '{VisibilityTimeout: "30", RedrivePolicy: ({deadLetterTargetArn: $dlq, maxReceiveCount: "3"} | tojson)}')
aws sqs set-queue-attributes --queue-url "$q_url" --attributes "$attrs"

# Raw delivery: SQS receives the event JSON itself, not an SNS envelope around it.
aws sns subscribe --topic-arn "$topic_arn" --protocol sqs --notification-endpoint "$q_arn" \
  --attributes RawMessageDelivery=true --return-subscription-arn --output text >/dev/null

# Single table: dedup markers (EVENT#id) + daily campaign counters (CAMPAIGN#id / DAY#date).
if ! aws dynamodb describe-table --table-name rewardstream >/dev/null 2>&1; then
  aws dynamodb create-table --table-name rewardstream \
    --attribute-definitions AttributeName=pk,AttributeType=S AttributeName=sk,AttributeType=S \
    --key-schema AttributeName=pk,KeyType=HASH AttributeName=sk,KeyType=RANGE \
    --billing-mode PAY_PER_REQUEST >/dev/null
  aws dynamodb wait table-exists --table-name rewardstream
fi
ttl=$(aws dynamodb describe-time-to-live --table-name rewardstream \
  --query TimeToLiveDescription.TimeToLiveStatus --output text)
if [ "$ttl" != "ENABLED" ]; then
  aws dynamodb update-time-to-live --table-name rewardstream \
    --time-to-live-specification Enabled=true,AttributeName=expires_at >/dev/null
fi

if ! aws s3api head-bucket --bucket rewardstream-lake-local >/dev/null 2>&1; then
  aws s3api create-bucket --bucket rewardstream-lake-local \
    --create-bucket-configuration LocationConstraint=ap-south-1 >/dev/null
fi

echo "RS_TOPIC_ARN=$topic_arn"
echo "QUEUE_URL=$q_url"
echo "DLQ_URL=$dlq_url"
echo "RS_TABLE=rewardstream"
echo "RS_LAKE_BUCKET=rewardstream-lake-local"
