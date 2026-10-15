# 4. Operational Excellence and Monitoring

## 4.1 How Kubernetes holds checkout up at ten times traffic

Kubernetes turns capacity from a decision made under pressure into a control loop. A **Deployment** states the intended result — "eight replicas of order at this digest" — and creates a **ReplicaSet** that keeps that many Pods running, so a Pod lost to a node failure at peak is replaced in seconds without anyone being paged.

**Probes** separate two questions the current platform conflates. The **readiness** probe on `/health/ready` asks whether this instance can serve traffic now, and a Pod that fails it leaves the load balancer but stays alive — right for a transient database problem. The **liveness** probe on `/health/live` asks only whether the process still functions, and failing it restarts the container. March's leaking process had neither, so nothing noticed and nothing acted.

**Requests and limits** make isolation real. Order requests 250 m CPU and 256 Mi, limited to 1 CPU and 512 Mi: the request is what the scheduler reserves, the limit stops one Pod starving its neighbours. A container exceeding its memory limit is killed — crude, but reliable containment of exactly the March failure mode.

**Autoscaling.** Normal peak is about 150 orders an hour, so ten times is 1,500; one Order Pod handles a measured 190 an hour at 60% CPU, giving a peak need of about twelve. The HorizontalPodAutoscaler runs `minReplicas: 6`, `maxReplicas: 40` at a 70% CPU target, with KEDA also scaling on JetStream consumer lag so a backlog causes growth. **Karpenter** provisions nodes that fit the pending Pods rather than growing a fixed group, and a `PodDisruptionBudget` of `minAvailable: 75%` across three zones stops a drain removing checkout capacity mid-sale.

**What runs where.** All six services are stateless Deployments; nothing holding state runs in the cluster, with PostgreSQL on RDS Multi-AZ, Valkey on ElastiCache and a managed three-node NATS cluster. Running a database inside Kubernetes is a second specialism with no business return.

**Reducing the Kubernetes risk.** The honest answer is to delay it. For two quarters everything runs on **AWS ECS Fargate**, which gives the primitives that matter — desired count, health checks, rolling updates, autoscaling — with no cluster to operate, while the platform pair runs EKS in staging and migrates one service at a time from quarter three. Images, probes and pipeline are identical either way, so the move is a change of scheduler, not of architecture. Alongside it: Notification migrates first, two developers are seconded to platform work with budgeted training, and everything is reviewed Terraform.

## 4.2 Logging, metrics, traces and alerting

Detection is the largest component of the 112-minute mean time to restore, so this is the first work. Every service writes one JSON object per line to stdout, with the fields of Table 8 filled in by the shared platform package so they cannot be forgotten.

Table: Table 8 — The mandatory structured log schema
| Field | Example | Why it is mandatory |
|---|---|---|
| `time` | `2026-10-15T08:55:00.892Z` | UTC with milliseconds, so lines from four services sort correctly |
| `level` | `info` | Filtering and alert routing |
| `service` | `inventory-service` | Which service, without inferring it from the text |
| `version` | `0.6.1` | Ties a change in behaviour to a release |
| `environment` | `production` | One backend, several environments |
| `correlation_id` | `a8c155c3-…` | **The field the whole design rests on:** one customer action, one identifier, every service |
| `order_id` | `e403d195-…` | Present whenever the work concerns an order |
| `event` | `stock.rejected` | A stable machine-readable name; alerts match on this, never on free text |
| `error`, `duration_ms` | – | The failure, and how long the work took |

![Figure 4 — Telemetry from the services and from the legacy core reaches one backend through a single OpenTelemetry Collector, which also removes personal data before anything leaves the cluster. The SLO and its two burn-rate alerts are defined once. The right-hand panel traces what the March incident would have looked like on this design.](diagrams/out/observability.png)

The tooling is **SigNoz**, self-hosted, with OpenTelemetry instrumentation in every service (ADR-0002), which keeps that instrumentation portable.

**The checkout SLO.** 99.9% of `POST /orders` requests return 2xx within 900 ms over a rolling 30 days — an error budget of 43 minutes 12 seconds a month. Worth stating to the board in those terms: the March incident alone would have consumed a month and a half of it.

Table: Table 9 — Two burn-rate alerts on that objective: they fire on how fast the budget is being consumed, not on a raw error count (Beyer et al., 2018).
| | Alert 1 | Alert 2 |
|---|---|---|
| Metric | Error-budget burn rate, checkout SLO | Same |
| Threshold | 14.4× | 6× |
| Duration | 5 minutes | 1 hour |
| Severity | 1 — page on-call by phone | 2 — Slack, acknowledge within the working day |
| First runbook step | Open the checkout board; if a deployment finished in the last 20 minutes, run `watch_rollout.sh order --max-error-rate 0` to force a rollback | Group 5xx by `service` and `version` over the hour to identify the release |

**The March incident, replayed.** At 09:12 the memory of `notification-service` climbs. Neither checkout alert fires, because Notification is out of the checkout path and the budget is untouched; what opens a ticket is the restart-loop alert on that one workload. The engineer filters on `service: notification-service`, sees `OOMKilled` against a rising memory curve, and confirms in a minute that checkout is unaffected: one correlation ID shows `order.accepted` and `payment.authorised` succeeding while `notification.sent` is absent. Nothing is lost — the events wait on the stream (Section 6.3). Fifty minutes of lost sales becomes a background fix in working hours.

## 4.3 Service mesh: what it offers, and why not yet

A service mesh puts a sidecar proxy beside every workload and routes service-to-service traffic through it, giving, without application code: **mutual TLS** with rotated certificates, so each end proves its identity; **timeouts and retries** per route; **circuit breaking and outlier detection** that eject a failing instance; and **weighted traffic shifting**, the prerequisite for a canary. It also yields uniform per-hop metrics, in any language.

**Decision: no mesh within the twelve months.** With six services it would solve problems MaltaShop does not yet have, while adding a control plane, a proxy in every task and a new failure mode for two people already learning a container platform. Each capability comes more cheaply at this size: timeouts, jittered retries and circuit breakers live in the gateway and the one shared Go package — exactly the coupling the monorepo of Section 2.3 makes cheap; encryption in transit is TLS at the gateway with VPC-private traffic behind it; and no canary is needed, because Payment uses a rolling update (Section 3.3).

Two triggers reverse this, both on the roadmap: past fifteen services, library-based resilience drifts faster than it can be reviewed; and when partners move from reading the catalogue to writing orders, per-hop identity stops being optional. Linkerd is then the candidate, chosen over Istio for the smaller operational surface — the criterion behind every other decision here.
