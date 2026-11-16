# Source me. Points CLI/SDK at Floci. Not secrets: Floci accepts any non-empty creds.
unset AWS_PROFILE AWS_SESSION_TOKEN
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test
export AWS_REGION=ap-south-1 AWS_DEFAULT_REGION=ap-south-1
export AWS_ENDPOINT_URL=http://localhost:4566
export KUBECONFIG="$HOME/.kube/rewardstream"
