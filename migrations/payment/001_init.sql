-- Payment service schema. No card number is ever stored here: only the reference
-- the external provider returns, which keeps the platform outside PCI DSS scope.
CREATE TABLE payment_intents (
    order_id     uuid PRIMARY KEY,
    customer_id  text        NOT NULL,
    amount_cents bigint      NOT NULL CHECK (amount_cents >= 0),
    currency     char(3)     NOT NULL DEFAULT 'EUR',
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE payments (
    payment_id     uuid PRIMARY KEY,
    order_id       uuid        NOT NULL UNIQUE,
    customer_id    text        NOT NULL,
    amount_cents   bigint      NOT NULL CHECK (amount_cents >= 0),
    state          text        NOT NULL,     -- AUTHORISED | DECLINED
    psp_reference  text,
    decline_reason text,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE outbox (
    id           bigserial PRIMARY KEY,
    subject      text        NOT NULL,
    body         jsonb       NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);
CREATE INDEX outbox_unpublished ON outbox (id) WHERE published_at IS NULL;

CREATE TABLE processed_events (
    event_id     uuid PRIMARY KEY,
    event_type   text        NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now()
);
