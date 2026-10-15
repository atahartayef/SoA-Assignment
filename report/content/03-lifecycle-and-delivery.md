# 3. Lifecycle and Delivery Management

## 3.1 The pipeline for the Order service

The present release process is the root of several problems in the case at once: three-weekly, manual, on a Friday evening, forty minutes of downtime each time. Long gaps make each release large, large releases are risky, and the risk is managed by releasing even less often.

![Figure 2 — The Order service pipeline, drawn as the directed graph it actually is rather than a line of stages. Lint and test start together; build begins as soon as both are green; integration and scanning run in parallel on the images that build produced. Three services then deploy automatically. Payment is the single manual job, because a bad payment release costs money rather than minutes. The lower panel shows the rolling update the release jobs perform.](diagrams/out/pipeline.png)

**Branching.** Trunk-based. Work reaches `main` directly or through a branch living under a day, with anything unfinished behind a feature flag. This is not a style preference: much of the seven weeks spent adding a second payment provider was merge and regression cost, which short-lived branches remove (Forsgren, Humble and Kim, 2018).

**Build once, promote everywhere.** `build-images` produces four images from one Dockerfile, tags each with the commit SHA and writes the digests to artefacts that every later job reads. Nothing is rebuilt, so what runs in production is byte-identical to what the integration job exercised, and "it passed in CI" becomes a claim about an artefact rather than a moment in time.

**Continuous deployment, with one exception.** Order, Inventory and Notification deploy automatically on a green pipeline; Payment waits for a person. The split is blast radius, not caution in general: a faulty Order release is caught by the rollout watcher and reversed in ninety seconds, whereas a faulty Payment release can take or fail to take real money in that time, through a provider MaltaShop does not control. The exception is reviewed once change failure rate stays below 15%.

Listing: Listing 1 — The build job from `.gitlab-ci.yml`. One Dockerfile, four images, and the digest that every later job deploys by.
```yaml
build-images:
  stage: build
  needs: [lint, unit-and-contract-tests]
  parallel:
    matrix:
      - SERVICE: [order, inventory, payment, notification]
  script:
    - docker build --build-arg SERVICE=$SERVICE -t $IMAGE_BASE/$SERVICE:$TAG .
    - docker push $IMAGE_BASE/$SERVICE:$TAG
    - docker inspect --format='{{index .RepoDigests 0}}' $IMAGE_BASE/$SERVICE:$TAG > digest-$SERVICE.txt
  artifacts:
    paths: [digest-*.txt]      # the digests every later stage deploys by
```

## 3.2 Testing the Order service

Table: Table 5 — The test pyramid for the Order service: what each level proves, where it runs, and its time budget
| Level | Count | What it proves | Pipeline job | Budget |
|---|---|---|---|---|
| Unit | ~90 | The saga's permitted transitions, the availability rule, envelope construction — no database, no broker | `unit-and-contract-tests`, every push | < 5 s |
| Contract | 3 assertions | That `OrderConfirmed` still carries what Notification needs | same job, in the consumer's package | < 1 s |
| Integration | ~20 | Order against a real PostgreSQL: the outbox commits with the order, the idempotency key holds, the advisory lock elects one publisher | same job, with a service container | < 60 s |
| Component | 10 | The whole choreography through Compose, including both failure paths | `compose-integration` | < 3 min |
| End-to-end | 2 | Place and confirm an order against the deployed environment | after the release jobs | < 2 min |

The shape matters because of 3.1: with no human gate, a red build is the only thing between a mistake and production, so the cheap levels must be trustworthy.

The contract test is the agreement between the two teams. `notification-service` is the consumer and owns `contracts/order-confirmed.v1.schema.json`; `order-service` is the provider. The test builds the event with the *production* envelope code and validates it against that schema, so a renamed field or a total silently formatted as `"191.90"` fails the Order pipeline before merge rather than when customers stop receiving e-mail.

## 3.3 Deployment strategy for the Payment service

Table: Table 6 — The three strategies, judged for Payment on this platform
| | Rolling | Blue-green | Canary |
|---|---|---|---|
| Extra infrastructure | None | A second full environment per release | Small |
| Cost at ten times peak | None | Doubles the most expensive hour of the year | Low |
| Time to roll back | ~90 s (previous task definition) | Seconds | Minutes |
| Mixed versions live | Yes, briefly | No | Yes, by design |
| Prerequisite | Readiness probes | A second environment and a switch | Weighted routing: a mesh or a smart gateway |
| Fit for a team of fourteen | Native to ECS and Kubernetes | Extra moving parts to build | Not available without a mesh (Section 4.3) |

**Chosen: rolling update.** Two reasons specific to MaltaShop. First, cost: blue-green doubles the running environment at exactly the moment capacity is most expensive, and the tooling budget is explicitly limited, whereas a rolling update with `minimumHealthyPercent: 100` and `maximumPercent: 200` never dips below full capacity and is already built into the platform. Second, canary needs weighted routing, which needs a mesh, which Section 4.3 argues against this year — adopting a mesh to serve a deployment strategy is the tail wagging the dog.

The honest weakness of rolling is that two Payment versions serve traffic for a few minutes. That is managed rather than ignored: Payment's provider contract is backward compatible for one release, and a change that cannot satisfy that is flagged in review and deployed by draining to zero out of hours.

**Rollback procedure.** (1) `watch_rollout.sh` queries the Payment 5xx rate every minute for fifteen minutes after the deployment finishes. (2) Above 0.5% it reads the previous task definition revision, which is still registered. (3) It calls `update-service` with that revision and waits for stability — about ninety seconds, because the previous image is still cached. (4) The pipeline job fails, so the commit is visibly not released. (5) An incident record opens with the failing requests' correlation IDs attached.

Table: Table 7 — Two DORA metrics, with MaltaShop's measured baseline and a twelve-month target
| Metric | Baseline today | Target | Measurement |
|---|---|---|---|
| Lead time for changes | 19 days median, from first commit to production | Under 1 working day for the extracted services | Commit timestamp to successful release job |
| Mean time to restore | 112 minutes median over the last six incidents | Under 30 minutes | Alert fired to service healthy again |

These two were chosen over deployment frequency because they answer the questions the case actually poses. Lead time measures "a payment provider took seven weeks" directly; mean time to restore measures "customers tell us first", and of its 112-minute median roughly 70 minutes is detection, which Section 4.2 attacks first.

![Figure 3 — The pipeline's verify, build and integration jobs run against the prototype on a development machine, using the same commands as the hosted runner. All nine checks pass, including the full choreographed saga with both failure paths.](evidence/screenshots/10_pipeline_run.png)
