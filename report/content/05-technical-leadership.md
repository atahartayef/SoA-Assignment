# 5. Technical Leadership and Collaboration

## 5.1 Briefing for the board

Our shop is one program: the catalogue, the basket, payments and e-mails are written, released and fail together. That is why eight times normal traffic closed the site for two hours, why an e-mail fault stopped customers paying us for fifty minutes in March, and why a second card provider took seven weeks.

I recommend separating six parts of it, and only six: orders, stock, payments, e-mail, recommendations and the partner connection.

The CFO's concern is the right one, and it is why the proposal is this small. Microservices go wrong when a company our size splits its software into dozens of pieces and finds it has bought dozens of things to run. Six is a number fourteen people can own, and we move more out of the old system only if the first six prove themselves.

What we get is measurable. We can add capacity to the till alone when a sale starts. A fault in e-mail can no longer stop a sale. We can release on any weekday without closing the shop, so a fix reaches customers the same day rather than three weeks later. And the recommendation feature can read what shoppers do without any route to the till.

What it costs: about €2,150 a month in tooling and cloud services, two developers on platform work for a year, and training. The real risk is learning a new way of running software while still running the shop, and we control it by doing less — six services, tools we can leave, and the old system kept beside the new so any step can be reversed.

The vendor's platform puts a licensed hub between every part of the system. It moves the bottleneck rather than removing it, and creates one component that can stop the whole shop.

## 5.2 How fourteen people work on six services and a legacy core

**Ownership.** Two stream-aligned teams of five — Checkout (Order, Inventory, Payment) and Experience (Notification, Recommendation, partner gateway) — plus a platform pair working with the two operations staff. The legacy core is owned jointly on a weekly rota: a rota is honest, whereas pretending a shrinking monolith has one owner is not. Whoever writes a service deploys it and is called when it breaks (Skelton and Pais, 2019).

**Git and review.** One repository, trunk-based, branches living under a day. Every change needs one approval; anything touching the payment path, a published contract or the anti-corruption layer needs two, one from the other team. Review is for design and risk — formatting, vetting and secret scanning are the pipeline's job.

**Contracts.** Each subject's payload has a JSON Schema in `contracts/`, owned by the consumer and tested in both pipelines (Section 3.2). Subjects are versioned in the name, and a breaking change publishes a new subject beside the old. The public API is OpenAPI with a six-month deprecation window, because thirty retailers depend on it.

**Documentation and on-call.** README, runbook and decision records live beside the code. On-call is one engineer per team on a weekly business-hours rota, with one platform rota overnight — what fourteen people can sustain. Every severity-1 incident gets a blameless review within a week, producing at most three actions with owners and dates.

Table: Table 10 — Three operational complexities this structure creates, and how each is managed
| Complexity | Why it arises | Strategy |
|---|---|---|
| In a choreography, no component knows the whole flow | Each service only reacts to what it subscribes to | The Order service records every event it observes as a timeline (Figure 7), so the saga is still answerable from one place; a consumer-lag dashboard shows where a flow has stalled; integration tests assert the whole sequence, not individual hops |
| Two teams and a jointly owned monolith invite "not our problem" | The legacy core has no single owning team | A weekly rota with a named steward; any change to the anti-corruption layer needs review from both teams; the core's shrinking module count is a standing agenda item |
| A shared platform package couples four services at build time | The monorepo decision in Section 2.3 | The package is versioned and changed by pull request like any other code; a change to it runs all four services' test suites; anything service-specific is forbidden in it, enforced in review |

## 5.3 Two architecture decision records

**ADR-0001: NATS JetStream as the messaging layer.**
*Context.* Six services need durable at-least-once delivery, retention long enough to replay a bad afternoon, several subscribers per event, and a footprint two staff can hold in their heads.
*Decision.* NATS JetStream: one stream, `MALTASHOP`, carrying `maltashop.>`, thirty days of file-backed retention, one durable pull consumer per service, a two-minute duplicate window on `Nats-Msg-Id`.
*Alternatives rejected.* **Kafka or MSK** — the strongest technically and the right answer once the browsing stream arrives, but partitions, consumer groups, rebalancing and a JVM to tune are a large first lesson. **RabbitMQ** — familiar, but a queue is consumed once, so three subscribers mean three queues and a fan-out exchange, and replay means republishing by hand. **A database table as a queue** — free and sufficient today, but every consumer then polls the producer's database, the coupling the migration exists to remove.
*Criteria.* Operational effort, durable replay, subscribers per event, cost, ordering, replaceability.
*Consequences.* One binary, one configuration file, one dashboard; a new consumer is a subscription, not new infrastructure; the duplicate window removes a class of double-publish bug. Against that, NATS has a far smaller ecosystem — fewer connectors, fewer engineers who know it, no mature schema registry — and ordering is per stream, not per order. **Debt accepted:** months of browsing events for the AI feature is a second decision deferred to phase three, and may be answered with Kafka alongside JetStream.

**ADR-0002: SigNoz as a single, self-hosted observability platform.**
*Context.* Logs are files on two machines, there is no alerting, and about 70 of the 112 minutes of mean time to restore is detection. Six services plus the legacy core are coming, the budget is small, and two people run everything.
*Decision.* SigNoz, self-hosted, as the one destination for logs, metrics and traces, with OpenTelemetry SDKs exporting through a Collector.
*Alternatives rejected.* **Grafana LGTM** — the most capable open-source option, but four components to install, upgrade, size and back up in the year of the platform migration. **Datadog** — fastest to adopt and the best correlation available, but per-host and per-gigabyte pricing made it larger than the rest of the tooling budget combined at ten times traffic. **Managed Elastic** — cheaper than Datadog, but still a monthly licence, and index lifecycle management is its own specialism.
*Criteria.* One tool rather than several, cost at ten times traffic, effort to operate, native OpenTelemetry ingestion, and moving from a log line to its trace.
*Consequences.* One place to look, one correlation ID, no licence cost, and — because instrumentation is OpenTelemetry, not a vendor agent — the backend can be replaced later without touching service code, which is the insurance actually bought. Against that, MaltaShop owns storage, upgrades and retention. **Debt accepted:** the legacy core emits no spans, so a trace stops at its edge until phase three, and fifteen days of trace retention is short for a GDPR audit.

These two decisions set the project's risk profile: messaging decides whether the AI feature is a subscription or a rewrite, telemetry decides whether fourteen people can run six services at all. Both debts carry a named trigger, because debt written down is a decision and debt that is not is a surprise found during a sale.
