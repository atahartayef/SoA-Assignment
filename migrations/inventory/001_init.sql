-- Inventory service schema: stock and the holds taken during open sagas.
CREATE TABLE stock (
    sku         text PRIMARY KEY,
    description text    NOT NULL,
    on_hand     integer NOT NULL CHECK (on_hand >= 0),
    held        integer NOT NULL DEFAULT 0 CHECK (held >= 0)
);

-- A hold is the "try" of try-confirm-cancel: stock promised, not yet taken.
CREATE TABLE holds (
    order_id   uuid PRIMARY KEY,
    lines      jsonb       NOT NULL,
    state      text        NOT NULL,      -- HELD | RELEASED | COMMITTED | REJECTED
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
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

INSERT INTO stock (sku, description, on_hand) VALUES
    ('FRIDGE-A3',    'Marsa A3 fridge-freezer',         9),
    ('BLENDER-PRO',  'Qormi Pro blender',              52),
    ('TOASTER-2S',   'Birgu two-slice toaster',       120),
    ('SPEAKER-BT',   'Comino Bluetooth speaker',       74),
    ('MIXER-900',    'Rabat 900 W stand mixer',        31),
    ('PREORDER-TAB', 'Xaghra tablet (pre-order)',       0);
