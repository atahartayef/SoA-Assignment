# 1. Architectural Evolution and Synthesis

## 1.1 The scalability ceiling, and what decomposition removes

The constraint is not that MaltaShop's software is large, but that one deployable unit and one MySQL schema force five unrelated workloads to share one set of limits. Browsing is read-heavy and cacheable, checkout is write-heavy and must never queue, notification is bursty and nobody waits for it; bound together, they are governed by the most constrained of them (Bass, Clements and Kazman, 2021).

Black Friday showed the cost. The only available response to eight times traffic was more copies of the whole application against the same database; each copy added connections, contention rose rather than fell, and checkout queued behind catalogue reads until the site was down for over two hours. The target is ten times traffic at 99.9% availability, which this shape cannot reach by being made larger.

Table: Table 1 — What each incident in the case reveals, and the specific property of a decomposed system that removes it
| What happened | The underlying coupling | What decomposition removes |
|---|---|---|
| 8× traffic, checkout queued, 2 hours down | One scaling unit, one connection pool | Checkout services scale alone; browsing is served by a separate service and a cache |
| E-mail memory leak stopped checkout for 50 minutes | One process, one address space | A leak kills one container; its events wait on the stream and orders still complete (Section 6.3) |
| A second payment provider took seven weeks | Payment logic entangled with orders and customers | One Payment service behind one interface; a provider is an adapter, not a migration |
| Releases every three weeks, 40 minutes offline | One release train for five capabilities | Each service releases on its own, with no downtime (Section 3) |
| Customers report incidents first | Logs on local disks, no telemetry | One place for logs, metrics and traces, with alerts on a stated objective (Section 4.2) |
| The AI model has no data path | Only the production database | A durable event stream the model subscribes to, with no change to checkout |

## 1.2 Three options, judged for a fourteen-person company

Table: Table 2 — The options against what MaltaShop must achieve in twelve months
| Criterion | Modular monolith, kept | The vendor's ESB platform | Microservices |
|---|---|---|---|
| Scale checkout alone | No | Partly; the bus is a shared ceiling | Yes |
| Weekly release per capability | No | Limited; changes queue behind the bus team | Yes |
| Fault isolation | Low | Medium; the bus is a single point of failure | High on asynchronous paths |
| Fit for the AI feature | Database polling | Vendor adapters | Subscribe to the stream |
| Integration logic | In-process | A central, intelligent bus (Erl, 2005) | Smart endpoints, dumb pipes (Lewis and Fowler, 2014) |
| New operational burden | None | Licensing, vendor skills, lock-in | A broker, a platform, telemetry |

The decisive facts are organisational. Fourteen people is a small organisation, and Fowler (2015) is explicit that microservices carry a premium that only repays above a certain size and skill level; MaltaShop sits near that line rather than comfortably above it. The temptation is to defer entirely, but two of the business goals cannot be met without independent deployment at all: weekly releases without downtime and ten-times checkout capacity are properties of the deployment unit, not of the code inside it.

The ESB resolves neither. It centralises routing in a component owned by one vendor and one team, so the queue for change moves rather than disappears, and it puts a new single point of failure in front of checkout. Its valuable ideas — explicit contracts, discoverability, reuse — survive without the bus.

**Recommendation: a deliberately small migration.** Extract six services, not sixteen, and only along the hot path: Order, Inventory, Payment, Notification, Recommendation and the partner gateway. Catalogue, Customer and reporting stay in the existing codebase, refactored into a modular monolith and treated as a service behind an anti-corruption layer (Fowler, 2004; Newman, 2019). It is the smallest change that meets every stated goal.

## 1.3 Distributed-systems principles, and the bill that comes with them

Three principles shape that recommendation. **Partial failure** is normal rather than exceptional (Kleppmann, 2017), so the synchronous path is reduced to one hop and a failing Notification or Recommendation service cannot hold a request open. **The network is unreliable**: a call may time out after succeeding and a message may arrive twice, so every write carries an idempotency key and every consumer records what it has handled (Section 6.1). **Eventual consistency** replaces the single transaction (Vogels, 2009); the customer is told "order received, confirming payment" for a second or two, which is what card payments already produce.

The bill is specific: network latency where there were function calls; no cross-service joins, so some reports are assembled rather than queried; debugging that needs correlation IDs and traces; versioned contracts; a broker to run; and a platform two staff must learn while still running the current one. Keeping the count at six is the single most effective control on all six costs, which is why it is the recommendation rather than an afterthought.
