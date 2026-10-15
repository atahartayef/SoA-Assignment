# 2. Scalable System Design

## 2.1 Six services, and a legacy core that shrinks

The decomposition follows one rule: extract a capability only when it has a scaling, failure or release requirement the monolith cannot meet. That gives six services and a legacy core, not a service per noun.

Table: Table 3 — The six extracted services, the legacy core, and why each store was chosen
| Component | Responsibility | Data owned | Store, and why |
|---|---|---|---|
| Edge gateway | TLS, routing, customer sessions, per-retailer API key, scope and rate limit | none | – |
| Order | Accepts the order, owns its lifecycle | orders, items, observed history, outbox | PostgreSQL — needs a local transaction around the order and its event |
| Inventory | Stock and the holds taken during open sagas | stock, holds | PostgreSQL — row locks make concurrent holds correct |
| Payment | The only caller of the card provider | intents, payments, provider references | PostgreSQL — never a card number, so PCI scope stays with the provider |
| Notification | E-mail and SMS, driven by events | message log | Valkey — written once, read by key, never joined; a relational store would be ceremony |
| Recommendation | Trains on events, serves suggestions | features, model artefacts | Object storage + Valkey online store |
| Partner gateway | The 30 retailers' API surface | API keys, quotas | PostgreSQL |
| **Legacy core** | Catalogue, Customer, reporting | the existing MySQL schema | MySQL — unchanged, behind an anti-corruption layer |

![Figure 1 — The target architecture after twelve months. Six services have been strangled out of the legacy core, which is still there, still PHP, and still holds Catalogue and Customer; it now sits behind an anti-corruption layer so that the new services never see its schema. Everything integrates through one NATS JetStream stream, which is also where the AI Recommendation service and the observability pipeline attach. Keeping one stream rather than one per event is what holds the operational surface down.](diagrams/out/architecture.png)

The AI Recommendation service never touches checkout. It consumes the same stream, reading `maltashop.order.*` and a browsing subject and writing features into its own store; the storefront asks it for suggestions through the gateway with a 200 ms timeout and a best-sellers fallback, so a slow model degrades one panel. With thirty days of messages retained, retraining replays the stream rather than requesting an export.

Authentication happens once, at the edge. Each retailer has its own API key bound to a scope (`catalogue:read`, `orders:write`) and a quota of 60 requests a minute, so one retailer's bug cannot exhaust capacity for the other twenty-nine. Services behind the gateway receive a signed internal token and re-check scope on writes.

## 2.2 Four patterns, and the work each one does

**Strangler fig with an anti-corruption layer.** The gateway routes a capability to a new service once it exists and to the legacy core until then (Fowler, 2004), while the anti-corruption layer translates between the old schema and the new model so the new services never inherit the old shape. This makes the migration reversible: if Order proves troublesome, routing goes back.

**Choreographed saga with try-confirm-cancel.** Order, Inventory and Payment can no longer share a transaction, so each reacts to events and publishes its own outcome: Inventory *holds* stock on `order.placed`, *confirms* on `order.confirmed` and *cancels* on `payment.declined` (Pardon and Pautasso, 2014). Choreography was chosen over orchestration because with three participants and one failure mode an orchestrator would be a component to build, deploy and operate for no decision it alone could make.

**Publish-subscribe on one durable stream.** Every integration is a subject on one JetStream stream, so adding the Recommendation service or the thirty-first retailer is a new durable consumer rather than a change to a producer.

**Rate limiting with bulkheads at the edge.** Partner, customer and storefront traffic each get a pool and quota at the gateway, so a partner batch job cannot consume the capacity checkout needs (Nygard, 2018).

Table: Table 4 — Interaction matrix. Only two interactions are synchronous, and both have a human waiting at the other end.
| From → To | Style | Why |
|---|---|---|
| Client → Gateway → Order | Synchronous | The customer is waiting for an order number |
| Order → Inventory | Asynchronous (event) | Checkout must not wait for a stock decision |
| Inventory → Payment | Asynchronous (event) | Payment reacts to `inventory.reserved`, which is what enforces the sequence |
| Payment → Order | Asynchronous (event) | The provider may take seconds; success and failure take the same path |
| Payment → Inventory | Asynchronous (event) | The compensating hand-back, triggered by `payment.declined` |
| Order → Notification | Asynchronous (event) | E-mail is never on the critical path |
| Order/browsing → Recommendation | Asynchronous (event) | The model must be unable to slow checkout |
| Gateway → Legacy core | Synchronous | A browsing request needs the catalogue now |
| Storefront → Recommendation | Synchronous, 200 ms timeout | A page is rendering; fallback is best-sellers |

## 2.3 Four of the twelve factors, and cloud-native fit

**I Codebase.** One repository holds the four services and the shared platform package, each with its own deployment. A monorepo is unusual advice here and it is deliberate: fourteen people maintaining four copies of logging, de-duplication and health-check code is a worse risk than the coupling one internal package creates (Ford, Parsons and Kua, 2017).

**II Dependencies.** Dependencies are declared in `go.mod` and locked in `go.sum`, so the build downloads exactly those versions and the final image is one static binary with no system libraries.

**V Build, release, run.** The three stages are strictly separate: the pipeline builds an image once, tagged by commit SHA; a release is that image plus an environment's configuration; a run is a task started from the release. Nothing is built during a deployment, so the artefact tested is the artefact that runs (Humble and Farley, 2010).

**X Dev/prod parity.** `docker compose up` starts the same images against PostgreSQL 16, Valkey 8 and JetStream at the same versions as production, differing only in replica counts and credentials. The gap the case describes, where releases are hand-applied to drifted servers, closes here.

These give the CNCF (2023) definition of cloud native something concrete: loosely coupled services, externally configured, in immutable containers, observable by default, safe to restart at any moment.
