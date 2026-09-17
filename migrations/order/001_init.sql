-- Order service schema. Owned by this service alone; no other service connects here.
CREATE TABLE orders (
    order_id        uuid PRIMARY KEY,
    customer_id     text        NOT NULL,
    state           text        NOT NULL,
    total_cents     bigint      NOT NULL CHECK (total_cents >= 0),
    currency        char(3)     NOT NULL DEFAULT 'EUR',
    idempotency_key text        NOT NULL UNIQUE,
    correlation_id  uuid        NOT NULL,
    failure_reason  text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE order_items (
    order_id   uuid    NOT NULL REFERENCES orders (order_id) ON DELETE CASCADE,
    sku        text    NOT NULL,
    quantity   integer NOT NULL CHECK (quantity > 0),
    unit_cents bigint  NOT NULL CHECK (unit_cents >= 0),
    PRIMARY KEY (order_id, sku)
);

-- What the order observed, in the order it observed it. In a choreography the
-- aggregate has no command log, so this history is the only saga trace there is.
CREATE TABLE order_history (
    id          bigserial PRIMARY KEY,
    order_id    uuid        NOT NULL REFERENCES orders (order_id) ON DELETE CASCADE,
    state       text        NOT NULL,
    note        text        NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now()
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

-- A local replica of the prices checkout needs, so placing an order never makes
-- a synchronous call to the Catalogue module inside the legacy core.
CREATE TABLE price_cache (
    sku        text PRIMARY KEY,
    unit_cents bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO price_cache (sku, unit_cents) VALUES
    ('FRIDGE-A3',    89900),
    ('BLENDER-PRO',  11450),
    ('TOASTER-2S',    3990),
    ('SPEAKER-BT',   12990),
    ('MIXER-900',     6750),
    ('PREORDER-TAB', 42900);
