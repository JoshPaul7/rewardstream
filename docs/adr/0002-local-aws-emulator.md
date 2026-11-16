# ADR-0002: Local AWS emulator for Tier 0

- Status: Accepted
- Date: 2026-11-16
- Deciders: Joshua Siby Paul

## Context

Tier 0 must run the full pipeline (SNS, SQS, DynamoDB, S3) locally and in CI
at $0, with no access to the real AWS account. The services use the AWS SDK
for Go v2, which honours `AWS_ENDPOINT_URL`, so any emulator that speaks the
AWS wire protocol on a local port can be swapped in without code changes.

On 23 March 2026 LocalStack consolidated into a single image that requires an
auth token, including in CI. The free Hobby plan is limited to non-commercial
use, and the open-source Community repository was archived. This repo is
public and its CI runs on GitHub Actions, so any emulator that needs a token
adds a secret to manage and a vendor account the pipeline depends on.

## Options considered

| Option | Summary | Deciding factor |
| --- | --- | --- |
| LocalStack (Hobby) | Mature, widest coverage; needs account + auth token | Token in CI for a public repo; "non-commercial" is ambiguous for a job-search portfolio |
| **Floci** | MIT-licensed, no account or token, drop-in on port 4566 | No secrets, no vendor dependency; younger project with known fidelity gaps |
| moto | Python library mocking boto3 in-process | Wrong shape for Go services; server mode would add a third tool |

## Decision

Use **Floci** as the Tier 0 emulator, pinned by tag and digest:

```
floci/floci:2.4.0@sha256:5ee85db604c4aa74ac76e07ea5cc83086f37bb472bf3dbb97e34019e48efae9c
```

Standard variant, release channel (not `nightly`, which tracks `main`; not
`-compat`, whose bundled CLI we don't need; not `-baseline`, which is ARM64-only).

Reasons, in order of weight:

1. **No secret in CI.** A public repo's pipeline runs without an auth token to
store, rotate or leak.
2. **No vendor-account dependency.** The March 2026 change broke every
pipeline pinned to LocalStack's `latest` tag. With no account, there is no
licensing question and no pricing page that can break CI.
3. **Cheap exit.** The services only know `AWS_ENDPOINT_URL`. Moving to
LocalStack is a one-line image change plus a CI secret.

Its low startup time and memory are supporting points, not the reason.

Hardening in `deploy/local/compose.yaml`: the port is bound to `127.0.0.1`
only, and the Docker socket is **not** mounted (it is root-equivalent on the
host, and none of the services in use need it).

## Measurements

Author's laptop, WSL2, Docker 29.1.5, image cached, 5 runs of
`compose down` → `compose up -d` → poll `curl localhost:4566` every 20 ms:

| Metric | Result |
| --- | --- |
| API answering HTTP after `compose up` | median **428 ms** (range 415–754; run 1 cold) |
| Docker container start (share of the above) | ~360 ms |
| Floci's own startup (difference) | ~65 ms (includes ≤20 ms polling resolution) |
| Idle memory | **26.5 MiB** (consistent across restarts) |
| Image size | **95 MB** pull, 377 MB on disk |

LocalStack figures quoted elsewhere (~3.3 s startup, ~1 GB image) are vendor
claims from Floci's README and were not measured here.

## Consequences

**Positive**

- Zero-cost, secret-free local and CI loop. A fresh runner pulls 95 MB.
- `memory` storage mode resets on every restart, so each test run starts clean.

**Negative, with mitigations**

| Risk | Mitigation |
| --- | --- |
| Fidelity gaps: an emulator proves the API calls, not exact AWS behaviour. Third-party reviews report DynamoDB conformance gaps. | Small contract-test suite against real AWS in Tier 1; AWS’s own DynamoDB Local as a targeted fallback for DynamoDB-specific gaps |
| **No IAM enforcement.** Dummy credentials resolve to `arn:aws:iam::000000000000:root`, so any IAM policy "works" locally. | IAM is validated only in Tier 1 / CI against real AWS, never inferred from Tier 0. |
| Misconfiguration could send "local" traffic to the real account (credential-chain fallback). | Services build every client from one shared `aws.Config` and, at startup, call STS `GetCallerIdentity`. They refuse to start unless the account matches the mode (`000000000000` locally), and also refuse on **any** error from that check (fail closed). |
| Young project; release cadence or maintenance could stop. | Pinned by digest, bumped deliberately via PR; one-line fallback to LocalStack. |
| Docker-backed features (e.g. Lambda execution) need the Docker socket, which is not mounted. | Revisit when the Lambda work starts; decide then whether to mount it or test Lambda another way. |
| Returned resource URLs use `localhost`, which breaks when the services run in their own containers. | Set `FLOCI_HOSTNAME=floci` once the services join the Compose stack. |

## Revisit if

- A Floci bug blocks an operation we depend on, with no fix or workaround in a release.
- Floci stops publishing releases or leaves security reports unaddressed for 3+ months.
- Local Lambda execution becomes necessary (Docker-socket trade-off).
- LocalStack's terms change so that a public, non-commercial portfolio can use it without CI friction.

## References

- Floci: https://github.com/floci-io/floci · image tags: https://floci.io/floci/configuration/docker-images/
- LocalStack 2026.03.0 release: https://blog.localstack.cloud/localstack-for-aws-release-2026-03-0/
- LocalStack licensing: https://docs.localstack.cloud/aws/licensing/
- AWS SDK endpoint configuration: https://docs.aws.amazon.com/sdkref/latest/guide/feature-ss-endpoints.html
