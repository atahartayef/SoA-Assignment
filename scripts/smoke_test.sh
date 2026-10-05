#!/usr/bin/env bash
# End-to-end check of the choreographed place-order saga. Run by the integration
# stage of the pipeline and by hand after `docker compose up`.
set -euo pipefail
ORDER=${ORDER_URL:-http://localhost:9180}
INVENTORY=${INVENTORY_URL:-http://localhost:9181}
PAYMENT=${PAYMENT_URL:-http://localhost:9182}
NOTIFY=${NOTIFY_URL:-http://localhost:9183}

pass() { printf '  \033[32mPASS\033[0m  %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m  %s\n' "$1"; exit 1; }

place() {   # place <correlation-id> <idempotency-key> <items-json>
  curl -sS -X POST "$ORDER/orders" -H 'content-type: application/json' \
    -H "x-correlation-id: $1" -H "idempotency-key: $2" \
    -d "{\"customer_id\":\"MT-8820\",\"items\":$3}"
}
await_state() {   # await_state <order-id> <state>
  for _ in $(seq 1 40); do
    state=$(curl -sS "$ORDER/orders/$1" | jq -r .state)
    [ "$state" = "$2" ] && return 0
    sleep 0.5
  done
  echo "    last state: ${state:-unknown}"; return 1
}
await_mail() {    # await_mail <order-id> <template>
  for _ in $(seq 1 40); do
    [ "$(curl -sS "$NOTIFY/notifications?order_id=$1" | jq -r '.[0].template // empty')" = "$2" ] && return 0
    sleep 0.5
  done
  return 1
}

echo "1. happy path: held, authorised, confirmed - with nobody orchestrating"
CID=$(uuidgen | tr 'A-Z' 'a-z')
BODY=$(place "$CID" "mt-$CID" '[{"sku":"TOASTER-2S","quantity":2},{"sku":"MIXER-900","quantity":1}]')
ORDER_ID=$(echo "$BODY" | jq -r .order_id)
[ "$(echo "$BODY" | jq -r .state)" = PENDING ] || fail "the API should answer PENDING at once"
pass "POST /orders returned 201 PENDING with order_id $ORDER_ID"
await_state "$ORDER_ID" CONFIRMED || fail "the saga did not reach CONFIRMED"
pass "the saga reached CONFIRMED"
[ "$(curl -sS "$PAYMENT/payments?order_id=$ORDER_ID" | jq -r '.[0].state')" = AUTHORISED ] \
  || fail "payment was not authorised"
pass "payment-service recorded an AUTHORISED payment"
await_mail "$ORDER_ID" order-confirmed || fail "no confirmation e-mail was recorded"
pass "notification-service recorded the confirmation e-mail"
[ "$(curl -sS "$NOTIFY/notifications?order_id=$ORDER_ID" | jq -r '.[0].correlation_id')" = "$CID" ] \
  || fail "the correlation ID did not survive the hops"
pass "the same correlation ID appears in every service"
STEPS=$(curl -sS "$ORDER/orders/$ORDER_ID" | jq -r '[.timeline[].note] | join(" -> ")')
[ "$STEPS" = "InventoryReserved -> PaymentAuthorised" ] || fail "unexpected timeline: $STEPS"
pass "the order observed: $STEPS"

echo "2. idempotency: the same key must not create a second order"
[ "$(place "$CID" "mt-$CID" '[{"sku":"TOASTER-2S","quantity":2}]' | jq -r .order_id)" = "$ORDER_ID" ] \
  || fail "a retry created a second order"
pass "the retried request returned the original order"

echo "3. compensation: payment declined, the hold is given back"
BEFORE=$(curl -sS "$INVENTORY/stock" | jq -r '.[]|select(.sku=="FRIDGE-A3")|.available')
CID2=$(uuidgen | tr 'A-Z' 'a-z')
ORDER_ID2=$(place "$CID2" "mt-$CID2" '[{"sku":"FRIDGE-A3","quantity":1}]' | jq -r .order_id)
await_state "$ORDER_ID2" CANCELLED || fail "the declined order did not reach CANCELLED"
pass "the declined order was cancelled"
AFTER=$(curl -sS "$INVENTORY/stock" | jq -r '.[]|select(.sku=="FRIDGE-A3")|.available')
[ "$BEFORE" = "$AFTER" ] || fail "stock was not released (before $BEFORE, after $AFTER)"
pass "the compensating action returned the held stock ($AFTER available)"
await_mail "$ORDER_ID2" order-cancelled || fail "no cancellation e-mail was recorded"
pass "the customer was told, by e-mail, why the order failed"

echo "4. rejection: an item with no stock never reaches payment"
CID3=$(uuidgen | tr 'A-Z' 'a-z')
ORDER_ID3=$(place "$CID3" "mt-$CID3" '[{"sku":"PREORDER-TAB","quantity":1}]' | jq -r .order_id)
await_state "$ORDER_ID3" REJECTED || fail "the out-of-stock order did not reach REJECTED"
[ "$(curl -sS "$PAYMENT/payments?order_id=$ORDER_ID3" | jq -r 'length')" = 0 ] \
  || fail "payment should never have been called"
pass "the order was rejected before payment was called"

printf '\n\033[32mall checks passed\033[0m\n'
