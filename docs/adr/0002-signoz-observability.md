# ADR-0002: SigNoz as a single OpenTelemetry-native observability platform

* Status: accepted
* Date: 2026-03-18
* Deciders: Lead Solutions Architect, Operations lead, Finance

## Context

Logs are files on two virtual machines. There is no dashboard, no alerting, and
customers usually report an incident before MaltaShop notices it. In March, a
memory leak in e-mail notifications took checkout down for 50 minutes, and most of
that time went on working out which process was at fault. After the migration there
will be six services plus the legacy core. Whatever is chosen has to be paid for
every month and operated by two people.

## Decision

Adopt **SigNoz**, self-hosted on the existing container platform, as the single
destination for logs, metrics and traces. Every service is instrumented with the
**OpenTelemetry** SDKs and exports through an OpenTelemetry Collector running as a
sidecarless daemon. Alerts are defined in SigNoz and routed to Slack and Opsgenie.

## Alternatives rejected

| Option | Why it was rejected |
|---|---|
| **Grafana LGTM, self-hosted** | The most capable open-source answer, but Loki, Mimir, Tempo and Grafana are four components to install, upgrade, back up and size. Four moving parts against two operations staff is the wrong ratio in a year that also contains a platform migration. |
| **Datadog** | The fastest to adopt and the best correlation available, but priced per host and per ingested gigabyte. Modelled at ten times traffic it was more than the rest of the tooling budget combined, and the cost grows precisely with the success the business is planning for. |
| **Elastic Observability (managed)** | Capable and cheaper than Datadog, but still a per-month licence, and index lifecycle management is its own specialism. Reconsider if self-hosting proves to be a distraction. |

## Criteria used

One tool rather than several (weight 3), cost at ten times traffic (3), effort to
operate (3), native OpenTelemetry ingestion so nothing is proprietary at the
instrumentation layer (3), the ability to jump from a log line to the trace it
belongs to (2).

## Consequences

**Good.** One place to look, one correlation ID, one alerting model, and the licence
cost is zero. Because instrumentation is OpenTelemetry and not a vendor agent, the
backend can be swapped for Grafana or Datadog later without touching service code -
which is the real insurance being bought here.

**Bad.** Self-hosting means MaltaShop owns the storage, the upgrades and the
retention policy; a ClickHouse disk filling up at midnight is now MaltaShop's
problem. SigNoz has a smaller community than Grafana, so answers are harder to find.

**Debt accepted.** Only the six extracted services are traced. The legacy core emits
structured logs and basic metrics but no spans, so a trace stops at its edge until
phase three. Retention is 15 days for traces and 30 for logs, which is short for a
GDPR audit request and will have to be revisited.
