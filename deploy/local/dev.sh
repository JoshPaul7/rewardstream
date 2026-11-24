# Load this in EVERY terminal you use for rewardstream:
#     source deploy/local/dev.sh
# It sets the Floci connection, the service settings, and helper commands.
# Why a file: shell functions and exports exist only in the terminal that
# created them, so every new terminal (and every rslog session) needs them again.

# Find this file's folder, wherever you run it from, and load the Floci settings.
_rs_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$_rs_dir/env.sh"

# Settings read by ingest-api and event-processor.
export RS_MODE=local
export RS_SHUTDOWN_DRAIN=1s
export RS_TOPIC_ARN=arn:aws:sns:ap-south-1:000000000000:rewardstream-events
export RS_QUEUE_URL=http://localhost:4566/000000000000/rewardstream-processing
export RS_DLQ_URL=http://localhost:4566/000000000000/rewardstream-processing-dlq
export RS_TABLE=rewardstream
export RS_LAKE_BUCKET=rewardstream-lake-local

# send <type> [reward_amount] [event_id]: POST one event to ingest-api.
send() {
  local type=$1 amt=${2:-0} id=${3:-$(cat /proc/sys/kernel/random/uuid)} extra=""
  [ "$amt" -gt 0 ] && extra=",\"reward_amount\":$amt"
  curl -s -w '  -> HTTP %{http_code}\n' -X POST localhost:8080/v1/events \
    -H 'Content-Type: application/json' \
    -d "{\"event_id\":\"$id\",\"type\":\"$type\",\"campaign_id\":\"cmp-42\",\"user_id\":\"usr-7\",\"occurred_at\":\"$(date -u +%FT%TZ)\"$extra}"
}

# counter: show today's (UTC) counters for campaign cmp-42 from DynamoDB.
counter() {
  aws dynamodb get-item --table-name "$RS_TABLE" --query Item \
    --key "{\"pk\":{\"S\":\"CAMPAIGN#cmp-42\"},\"sk\":{\"S\":\"DAY#$(date -u +%F)\"}}"
}

# lake: list every object in the S3 lake and print its contents.
lake() {
  aws s3 ls "s3://$RS_LAKE_BUCKET" --recursive | while read -r _ _ _ key; do
    echo "== $key"
    aws s3 cp "s3://$RS_LAKE_BUCKET/$key" -
  done
}

echo "rewardstream local env loaded: Floci at $AWS_ENDPOINT_URL, mode=$RS_MODE"
