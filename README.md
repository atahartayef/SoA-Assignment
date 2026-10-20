# MaltaShop lean platform

A running slice of the lean MaltaShop migration described in the Unit 5 report:
the **place-an-order** flow built as a **choreographed saga** over **NATS
JetStream**, in Go.

There is no orchestrator. Each service reacts to what has happened and publishes
what it decided. Nobody is in charge of the sequence, and the order in which the
steps happen is a property of who subscribes to what.

It exists to prove four claims the report makes:

1. checkout answers in milliseconds and keeps working when a service behind it dies;
2. Order, Payment and Inventory agree without a shared database and without a
   distributed transaction;
3. a failed step is undone by a **compensating action**, published by the service
   that owns the step, not commanded by a coordinator;
4. one customer action can be followed through every service by one **correlation ID**.

---

## The services

| Service | Port (host) | Store | Reacts to | Publishes |
|---|---|---|---|---|
| `order-service` | 9180 | `order-db` (PostgreSQL) | inventory and payment outcomes | `order.placed`, `order.confirmed`, `order.cancelled` |
| `inventory-service` | 9181 | `inventory-db` (PostgreSQL) | `order.placed`, `payment.declined`, `order.confirmed` | `inventory.reserved`, `inventory.rejected`, `inventory.released` |
| `payment-service` | 9182 | `payment-db` (PostgreSQL) | `order.placed`, `inventory.reserved` | `payment.authorised`, `payment.declined` |
| `notification-service` | 9183 | `notification-store` (Valkey) | `order.confirmed`, `order.cancelled` | – |
| `nats` | 4322, 8322 | file-backed JetStream | – | – |

Four services, four private stores, one stream. No service reads another
service's data.

### The saga, as choreography

```
POST /orders
   └─ order.placed ──┬──► inventory: hold the stock  ──► inventory.reserved ──► payment: authorise
                     └──► payment:  note the amount                                   │
                                                                        ┌─────────────┴─────────────┐
                                                        payment.authorised                payment.declined
                                                                │                                  │
                                       order: CONFIRMED ────────┘                                  ▼
                                                │                              inventory: release the hold
                                         order.confirmed                             inventory.released
                                                │                                           │
                                        inventory: commit                           order: CANCELLED
                                        notification: e-mail                         order.cancelled
                                                                                    notification: e-mail
```

Reserving stock is the **try** of try-confirm-cancel. `inventory.released` is the
**cancel**; committing on `order.confirmed` is the **confirm**. Payment never
listens to `order.placed` for its decision - only to `inventory.reserved` - which
is how the sequence is enforced without anyone enforcing it.

### Subjects

All eight live on one stream, `MALTASHOP`, subject `maltashop.>`:

`maltashop.order.placed` · `maltashop.order.confirmed` · `maltashop.order.cancelled` ·
`maltashop.inventory.reserved` · `maltashop.inventory.rejected` ·
`maltashop.inventory.released` · `maltashop.payment.authorised` ·
`maltashop.payment.declined`

Each service has one durable pull consumer named after it, filtered to the
subjects it needs, so a restart resumes exactly where it stopped.

---

## Running it

Requirements: Docker with Compose v2, plus `curl`, `jq` and `uuidgen`.

```bash
git clone https://github.com/atahartayef/SoA-Assignment.git
cd SoA-Assignment
cp .env.example .env            # optional; the defaults work
docker compose up --build -d --wait
./scripts/smoke_test.sh
```

Remove everything, volumes included:

```bash
docker compose down -v
```

### Try it by hand

```bash
curl -sS -X POST http://localhost:9180/orders \
  -H 'content-type: application/json' \
  -H 'x-correlation-id: 9f1c0000-1111-2222-3333-444455556666' \
  -H 'idempotency-key: demo-001' \
  -d '{"customer_id":"MT-8820","items":[{"sku":"TOASTER-2S","quantity":2},
                                        {"sku":"MIXER-900","quantity":1}]}' | jq

curl -sS http://localhost:9180/orders/<order_id> | jq '{state, timeline}'
curl -sS 'http://localhost:9183/notifications?order_id=<order_id>' | jq
curl -sS http://localhost:9181/stock | jq
```

Two SKUs make the failure paths visible:

* **`FRIDGE-A3`** costs €899, above the stubbed card limit of €750, so payment is
  declined and the hold is released (`CANCELLED`);
* **`PREORDER-TAB`** has no stock, so the saga stops before payment is ever
  involved (`REJECTED`).

Look at the stream itself:

```bash
docker run --rm --network maltashop-lean_default natsio/nats-box:0.14.5 \
  nats --server nats://nats:4222 stream info MALTASHOP
docker run --rm --network maltashop-lean_default natsio/nats-box:0.14.5 \
  nats --server nats://nats:4222 consumer report MALTASHOP
```

### Endpoints

| Method | Path | Service |
|---|---|---|
| `POST` | `/orders` | order — accepts `Idempotency-Key` and `X-Correlation-Id`, answers `201` with `state: PENDING` |
| `GET` | `/orders/{id}` | order — the order, its lines and the timeline it observed |
| `GET` | `/stock` | inventory — on hand, held and available per SKU |
| `GET` | `/payments?order_id=` | payment — the decision and the provider reference |
| `GET` | `/notifications?order_id=` | notification — the messages recorded for one order |
| `GET` | `/health/live` | all — is the process alive? |
| `GET` | `/health/ready` | all — can it reach its store? |

---

## Environment variables

Every setting is read from the environment at start-up (12-factor III); nothing is
read from a file. `.env.example` is the complete list.

| Variable | Default | Meaning |
|---|---|---|
| `SERVICE_NAME` | per service | The `service` field on every log line. |
| `SERVICE_VERSION` | `0.6.1` | Stamped on logs, so a bad release stands out. |
| `ENVIRONMENT` | `local` | `local`, `staging` or `production`. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `PORT` | 8080–8083 | Port inside the container. |
| `DATABASE_URL` | per service | That service's own PostgreSQL instance. |
| `REDIS_URL` | notification only | The Valkey keyspace it owns. |
| `NATS_URL` | `nats://nats:4222` | JetStream server. |
| `OUTBOX_POLL_MS` | `350` | How often the outbox leader drains the table. |
| `CARD_LIMIT_EUR` | `750` | The stubbed provider's limit, used to show compensation. |

In staging and production these come from AWS Systems Manager Parameter Store at
task start. No secret is committed.

---

## How consistency is kept without a distributed transaction

| Problem | Mechanism | Where |
|---|---|---|
| An event must not be sent for a change that was rolled back | **Transactional outbox** written inside the business transaction | `internal/platform/outbox.go` |
| Two replicas publishing the same outbox row | **PostgreSQL advisory lock**: one leader drains, the rest poll and find it taken | `outboxLockID`, `drain()` |
| The same publish retried | **JetStream `Nats-Msg-Id`** de-duplication, two-minute window | `Stream.Publish` |
| The same event redelivered | **Idempotent consumer**: `processed_events` claims each event once; Valkey `SET NX` in the service with no database | `Claim()`, `notification/main.go` |
| A late or repeated event corrupting an order | **Explicit state machine**: an event that does not fit the state is logged and dropped | `cmd/order/events.go` |
| Stock promised to an order that then fails | **Try-confirm-cancel**: hold, then either commit or release | `cmd/inventory/main.go` |
| The customer presses "Pay" twice | **Idempotency key**, unique in the database | `cmd/order/main.go` |

## Tests

```bash
go test ./... -race
./scripts/smoke_test.sh        # needs the stack running
./scripts/run_pipeline.sh      # every pipeline job, locally
```

* **Unit** — the saga state machine (`cmd/order`), the availability rule
  (`cmd/inventory`), the card limit (`cmd/payment`), the event envelope
  (`internal/platform`). No database, no broker.
* **Contract** — `contracts/contract_test.go` validates the real `OrderConfirmed`
  event, built by the production code, against the JSON Schema the consumer owns.
* **Integration** — `scripts/smoke_test.sh` drives the whole system, including
  both failure paths.

## Layout

```
.gitlab-ci.yml        verify, build, integration, scan, release
cmd/<service>/        one main package per service
internal/platform/    config, logging, database, outbox, JetStream, HTTP
contracts/            the schema the consumer owns, and its test
migrations/<service>/ that service's schema; nobody else connects to it
Dockerfile            one file, four images, selected by --build-arg SERVICE
scripts/              smoke test, local pipeline, deploy and rollback
docs/adr/             architecture decision records
evidence/             screenshots and transcripts of the demonstration
report/               the build pipeline for the Unit 5 report
```
