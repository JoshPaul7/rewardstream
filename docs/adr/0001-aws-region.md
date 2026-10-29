# ADR-0001: AWS region

- Status: Accepted
- Date: 2026-10-28
- Deciders: Joshua Siby Paul

## Context

rewardstream is a personal learning and portfolio project. It has no end users, no data-residency obligations and no availability SLA. Every resource has to be justified on cost. The project runs in a pre-existing AWS account with **no Free Tier credits**, so every billable hour is paid from day one.

The forces that matter:

- **Cost.** The Tier 1 target is $0–10/month. Compute and the single public IPv4 address dominate that number.
- **Developer-loop latency.** I work from Bengaluru, and kubectl, SSH, Terraform plans and Grafana are all used interactively.
- **Service availability.** The design needs EC2 (Graviton/ARM), EKS with Pod Identity, SQS, SNS, DynamoDB, S3, Lambda on arm64 and EMR Serverless (optional).
- **Spot capacity.** Tier 1 runs on spot instances, so capacity and interruption behaviour matter.

### Data gathered (checked 2026-10-27/28; spot data changes over time)

| Factor | ap-south-1 (Mumbai) | us-east-1 (N. Virginia) |
|---|---|---|
| t4g.small on-demand | $0.0112/hr (~$8.18/mo) | $0.0168/hr (~$12.26/mo) |
| Public IPv4 | $0.005/hr (~$3.65/mo), same in every region | same |
| Required services available | Yes | Yes |
| Spot placement score, t4g.small only (capacity 1) | 3/10 | 3/10 |
| Spot placement score, 5 ARM types (t4g.small, t4g.medium, m6g/m7g/c7g.medium) | 9/10 | 9/10 |
| t4g.small interruption frequency (Spot Instance Advisor, 30-day avg) | 10–15% | 10–15% |
| Spot savings vs on-demand (Spot Instance Advisor) | ~55% | ~57% |
| Latency from Bengaluru | Low | High (~200 ms+) |

## Options considered

1. **ap-south-1 (Mumbai):** close to me, with the lowest on-demand price for the instance family.
2. **us-east-1 (N. Virginia):** the largest region; new services and features usually launch there first. Most tutorials and examples default to it.
3. **eu-central-1 (Frankfurt):** the natural choice for a workload serving EU users, such as an adjoe-like company in Hamburg.

## Decision

Use **ap-south-1** for all regional resources.

It gives the lowest compute price of the options considered and the best interactive latency for me. Spot placement scores and interruption rates were identical to us-east-1 for the instance types we plan to use, so us-east-1 had no capacity advantage to make up for its higher price and latency.

eu-central-1 is rejected for this project because there are no EU users and no data-residency requirement. For a production workload serving EU users, it would be the default choice, for latency and GDPR data residency.

## Consequences

**Positive**
- About 33% cheaper on-demand compute than us-east-1 for t4g.small.
- Fast feedback for interactive work (kubectl, SSH, Grafana, terraform plan).

**Negative / risks**
- **Single-type spot is unreliable in both regions.** A placement score of 3/10 and an interruption rate of 10–15% mean a single t4g.small spot node will be reclaimed regularly. This is not a regional problem, but it constrains the Tier 1 design: the k3s node must run in an Auto Scaling group that accepts several ARM instance types, and no unrecoverable state may live on the node.
- **Features sometimes arrive later.** New AWS features and instance types can reach us-east-1 before ap-south-1. Mitigation: check regional availability before adopting anything new.
- **Examples need adapting.** Most documentation and examples assume us-east-1, so ARNs, endpoints and AMI lookups must not be copied blindly.

**Revisit this decision if**
- the project ever serves real users (pick the region closest to them, e.g. eu-central-1 for EU users),
- a required service or feature turns out to be missing from ap-south-1, or
- spot interruptions in ap-south-1 become materially worse than in other regions for the instance pools we use.
