# ADR-0001: NATS JetStream as the messaging layer

* Status: accepted
* Date: 2026-03-04
* Deciders: Lead Solutions Architect, Operations lead, Finance

## Context

The lean migration adds six services over twelve months and keeps the rest of the
platform inside a modular legacy core. The two operations staff who run the
existing virtual machines will also run whatever we choose, alongside their current
work. The tooling budget is small, and the team has never operated a broker. What
is actually needed is durable at-least-once delivery, retention long enough to
replay a bad afternoon, several independent subscribers to one event, and an
operational footprint one person can hold in their head.

## Decision

Use **NATS with JetStream**: a single stream, `MALTASHOP`, carrying
`maltashop.>`, with one durable pull consumer per service filtered to the subjects
it cares about. Thirty days' file-backed retention and a two-minute duplicate
window on `Nats-Msg-Id`. One server locally, three in production.

## Alternatives rejected

| Option | Why it was rejected |
|---|---|
| **Apache Kafka (or MSK)** | The strongest answer technically, and the one to revisit when the browsing stream arrives. But it brings partitions, consumer groups, rebalancing, ZooKeeper-or-KRaft decisions and a JVM to tune. Against two operations staff in their first year of distributed systems, that is the dominant risk, not the dominant benefit. |
| **RabbitMQ** | Familiar and well documented, but a classic queue is consumed once. Three subscribers to one event means three queues and a fan-out exchange to maintain, and replaying last Tuesday means republishing by hand. |
| **A database table as a queue** | Zero new infrastructure, and genuinely sufficient at today's volume. Rejected because it makes every consumer poll the writer's database, which is exactly the coupling the migration exists to remove. |

## Criteria used

Operational effort for two staff (weight 3), durable replay (3), several
subscribers per event (3), cost (3), delivery ordering (2), how easily it can be
replaced later (2).

## Consequences

**Good.** One binary, one configuration file, one dashboard. Subject wildcards mean
a new consumer is a new subscription, not new infrastructure. The duplicate window
removes a whole class of double-publish bugs. Running it costs the price of three
small instances.

**Bad.** NATS has a much smaller ecosystem than Kafka: fewer connectors, fewer
engineers who already know it, no mature schema registry. There is no partition
model, so ordering is per stream, not per order, and very high throughput would
need the stream split by hand.

**Debt accepted.** The AI recommendation feature will want months of browsing
events, not thirty days of order events. That is a second decision, deliberately
deferred to phase three, and it may well be answered with Kafka alongside - not
instead of - JetStream.
