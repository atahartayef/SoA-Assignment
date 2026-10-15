# 6. Implementation and Prototyping

## 6.1 Consistency across Order, Payment and Inventory

With one database per service, a single transaction across the three is gone. The prototype replaces it with a **choreographed saga**: a chain of local transactions, each published as an event and each with a compensating action that undoes it semantically (Garcia-Molina and Salem, 1987). There is no orchestrator — each service subscribes to what happened and publishes what it decided.

![Figure 5 — The place-order flow as choreography. The order of the steps is not enforced by a coordinator but by who subscribes to what: Payment listens for inventory.reserved and never for order.placed, so payment cannot be taken for stock that was never held. The lower band is the compensating path, which is the same mechanism running in the other direction.](diagrams/out/saga.png)

Holding stock is a **try-confirm-cancel** step (Pardon and Pautasso, 2014): Inventory *tries* on `order.placed` by raising a `held` count, *confirms* on `order.confirmed` by converting the hold to a deduction, and *cancels* on `payment.declined` by handing it back. A hold is a semantic lock — a visible row, not a database lock — so nothing is held across a network call.

Three mechanisms make it safe. **First, the transactional outbox**, so no event describes a change that was rolled back:

```go
return platform.InTx(ctx, s.pool, func(tx pgx.Tx) error {
    // ... insert the order and its items ...
    envelope, err := platform.NewEnvelope(s.cfg.ServiceName, "OrderPlaced", correlationID, "", ...)
    if err != nil { return err }
    // The event joins the outbox inside this transaction: it is sent if and
    // only if the order row is committed.
    return platform.Enqueue(ctx, tx, platform.SubjectOrderPlaced, envelope)
})
```

One replica drains that outbox, elected by a PostgreSQL advisory lock; sharing the work with `FOR UPDATE SKIP LOCKED` would give more throughput but let two publishers interleave one order's events, and at 1,500 orders an hour ordering is worth more.

**Second, de-duplication on both sides.** A publish carries the event ID as `Nats-Msg-Id`, which JetStream discards inside a two-minute window, so a publisher retry is dropped by the server. A consumer then claims the same ID once in its own store before acting:

```go
tag, err := tx.Exec(ctx,
    `INSERT INTO processed_events (event_id, event_type) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
    envelope.EventID, envelope.EventType)
return tag.RowsAffected() == 1, err   // false on a redelivery: the handler returns without acting
```

**Third, an explicit state machine**, so a late or repeated event cannot corrupt an order:

```go
var allowed = map[string]map[string]string{
    "PENDING":        {"InventoryReserved": "STOCK_RESERVED", "InventoryRejected": "REJECTED"},
    "STOCK_RESERVED": {"PaymentAuthorised": "CONFIRMED", "PaymentDeclined": "COMPENSATING"},
    "COMPENSATING":   {"InventoryReleased": "CANCELLED"},
}
```

JetStream supplies the rest: a durable pull consumer per service resumes where it stopped; explicit acknowledgement with a thirty-second wait and backoff of 1 s, 3 s and 10 s redelivers anything a handler did not finish; `MaxDeliver: 6` stops a poison message cycling forever. An `Idempotency-Key` with a unique constraint means a customer pressing "Pay" twice receives the same order.

## 6.2 One Dockerfile, four images, and what production adds

Listing: Listing 2 — The runtime stage of the shared `Dockerfile`; the full two-stage file is in Appendix B
```dockerfile
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
ARG SERVICE
ARG PORT=8080
ENV SERVICE_NAME=${SERVICE}-service PORT=${PORT}
COPY --from=build /out/service /app/service
USER 65532:65532
HEALTHCHECK --interval=15s --timeout=4s --start-period=12s --retries=3 \
  CMD ["/app/service", "-healthcheck"]
ENTRYPOINT ["/app/service"]
```

**Base image.** `gcr.io/distroless/static-debian12:nonroot` holds the binary, CA certificates and time-zone data — no shell, no package manager, no busybox — so an attacker who achieves execution finds nothing to run. At about 12 MB it also makes a rolling update fast.

**Multi-stage, one file for four services.** The build stage compiles with `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`: a static binary with no libc dependency, no build paths, no symbol table. `--build-arg SERVICE` selects the `main` package, so there is one Dockerfile to review rather than four that drift apart.

**Non-root.** The default user is `nonroot` (uid 65532), named explicitly so a later edit cannot silently return to root.

**Health checks.** Distroless has no `curl`, so the binary probes itself: `service -healthcheck` requests its own `/health/ready` and exits 0 or 1. That one endpoint serves the Docker check, Compose `--wait` and the Kubernetes readiness probe, so "healthy" means one thing everywhere.

**Configuration.** Every setting is read from the environment at start-up and listed in `.env.example`, which is what allows the identical image to run on a laptop and in production.

Table: Table 11 — What production needs beyond Compose, and what each addition contributes
| Tool | What it adds |
|---|---|
| ECS Fargate, then EKS with Karpenter | Desired-count reconciliation, rolling updates, autoscaling and node supply — none of which Compose has |
| Terraform | The environment is reviewable code, and an environment can be rebuilt rather than repaired |
| GitLab CI and its registry | The pipeline of Section 3 and immutable, digest-addressed images |
| RDS Multi-AZ, ElastiCache, managed NATS | Backups, failover and patching for everything that holds state |
| AWS Parameter Store with task-level IAM | Secrets injected at task start, never in a `.env` file or an image |
| OpenTelemetry Collector and SigNoz | The telemetry of Section 4.2, which Compose does not provide |
| Trivy and gitleaks in the pipeline | Vulnerable images and committed secrets stopped before release |

## 6.3 Evidence, and an honest reading of it

![Figure 6 — Placing an order. The service answers 201 Created in about five milliseconds with state PENDING, echoing the correlation ID the client sent. Nothing downstream has happened yet, which is the point.](evidence/screenshots/02_order_request.png)

![Figure 7 — The same order after the choreography has run. The timeline is what the Order service observed — InventoryReserved, then PaymentAuthorised — rather than steps it commanded, which is the practical difference between choreography and orchestration.](evidence/screenshots/03_order_timeline.png)

![Figure 8 — The Notification service's record of the same order, read from its Valkey keyspace, carrying the correlation ID the customer's request carried.](evidence/screenshots/04_event_consumed.png)

![Figure 9 — The logs of all four services filtered to one correlation ID. One customer action reads as a single sequence across four processes and four stores, which is the capability missing from the platform today.](evidence/screenshots/05_correlation_logs.png)

![Figure 10 — Fault isolation, measured. With notification-service stopped, checkout still answers 201 in under five milliseconds and the consumer report shows one message unprocessed for that consumer alone; every other consumer is at zero. Figure 11 shows the service restarting and the backlog clearing. This is the March incident with the damage removed.](evidence/screenshots/07_fault_isolation.png)

![Figure 11 — After the restart the unprocessed count returns to zero and the e-mails are recorded, late and complete.](evidence/screenshots/08_recovery.png)

**What it proves.** Checkout is one fast synchronous step; a service can die without taking a sale with it, and its work resumes; stock held for an order that later fails payment is returned by a compensating action (Figure 12); a retry yields one order; and a choreography with no coordinator still runs its steps in order and stays answerable from one place.

![Figure 12 — The compensating path. Payment declines the €899 fridge-freezer, Inventory hands the hold back, availability returns to 9, and the customer is told why.](evidence/screenshots/06_compensation.png)

**Where it falls short.** One NATS node at replica count one, so no failure of the messaging layer itself is tested. No gateway, API keys or legacy core, so nothing about the thirty retailers or the anti-corruption layer is shown. A stubbed card provider, so no third-party timeout is exercised. And no load test: five milliseconds is one request on an idle machine.

**What would change before production.** A three-node JetStream cluster at replica count three. A dead-letter subject with an alert on its depth, because the prototype relies on `MaxDeliver`. Real OpenTelemetry spans rather than correlation IDs alone, so a slow hop can be seen and not only followed. A load test against Section 4.1's policy, since twelve Pods is arithmetic, not measurement. And a stalled-saga sweeper: an order whose `inventory.reserved` is never answered stays `STOCK_RESERVED` indefinitely — the clearest weakness choreography introduces, and the strongest argument for an orchestrator later.
