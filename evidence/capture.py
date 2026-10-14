#!/usr/bin/env python3
"""Runs the demonstration behind Sections 3 and 6.3 and saves the evidence.

    python3 evidence/capture.py

Every screenshot renders the real output of the command printed above it, and the
same text is kept in evidence/transcripts/. Needs Docker, curl, jq and Playwright.
"""
import html
import json
import subprocess
import time
import uuid
from pathlib import Path

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parent.parent
EVIDENCE = ROOT / "evidence"
TRANSCRIPTS = EVIDENCE / "transcripts"
SHOTS = EVIDENCE / "screenshots"
ORDER = "http://localhost:9180"
INVENTORY = "http://localhost:9181"
PAYMENT = "http://localhost:9182"
NOTIFY = "http://localhost:9183"
BASKET = '{"customer_id":"MT-8820","items":[{"sku":"TOASTER-2S","quantity":2},{"sku":"MIXER-900","quantity":1}]}'
BIG = '{"customer_id":"MT-8820","items":[{"sku":"FRIDGE-A3","quantity":1}]}'
NATS = ("docker run --rm --network maltashop-lean_default natsio/nats-box:0.14.5 "
        "nats --server nats://nats:4222")

# A light terminal, to keep this submission's figures visually its own.
TERMINAL = """<!doctype html><html><head><meta charset="utf-8"><style>
 body {{ margin:0; padding:20px; background:#ffffff; }}
 .window {{ width:{width}px; border:1px solid #c9ced6; border-radius:6px; overflow:hidden;
            background:#fbfcfd; box-shadow:0 1px 3px rgba(20,28,40,.12); }}
 .bar {{ height:30px; background:#eef1f5; border-bottom:1px solid #d7dce3; display:flex;
         align-items:center; padding:0 13px; }}
 .dot {{ width:10px; height:10px; border-radius:50%; margin-right:7px; border:1px solid rgba(0,0,0,.12); }}
 .title {{ color:#48515d; font:12.5px/1 -apple-system, Helvetica, sans-serif; margin-left:11px; }}
 pre {{ margin:0; padding:14px 17px 17px; color:#1b2027; font:13.5px/1.5 Menlo, monospace;
        white-space:pre-wrap; word-break:break-all; }}
 .cmd {{ color:#1f5f99; font-weight:600; }} .hl {{ background:#fff0b3; font-weight:bold; }}
 .ok {{ color:#0b7a3b; }}
</style></head><body><div class="window"><div class="bar">
 <span class="dot" style="background:#ff6058"></span><span class="dot" style="background:#ffbd2e"></span>
 <span class="dot" style="background:#28ca42"></span><span class="title">{title}</span></div>
 <pre>{body}</pre></div></body></html>"""


def sh(command, check=True):
    result = subprocess.run(["bash", "-c", command], cwd=ROOT, capture_output=True, text=True)
    if check and result.returncode != 0:
        raise SystemExit(f"command failed: {command}\n{result.stdout}{result.stderr}")
    return (result.stdout + result.stderr).rstrip("\n")


class Terminal:
    def __init__(self, browser):
        self.browser = browser

    def capture(self, name, title, commands, highlight=None, width=1000):
        lines = []
        for command in commands:
            lines.append(f"$ {command}")
            output = sh(command)
            if output:
                lines.append(output)
            lines.append("")
        text = "\n".join(lines).rstrip("\n")
        (TRANSCRIPTS / f"{name}.txt").write_text(text + "\n", encoding="utf-8")
        body = "\n".join(
            f'<span class="cmd">{html.escape(row)}</span>' if row.startswith("$ ")
            else f'<span class="ok">{html.escape(row)}</span>' if row.strip().startswith("PASS")
            else html.escape(row) for row in text.splitlines())
        for mark in ([highlight] if isinstance(highlight, str) else (highlight or [])):
            body = body.replace(html.escape(mark), f'<span class="hl">{html.escape(mark)}</span>')
        page = self.browser.new_page(device_scale_factor=2, viewport={"width": width + 44, "height": 400})
        page.set_content(TERMINAL.format(width=width, title=html.escape(title), body=body))
        page.locator(".window").screenshot(path=str(SHOTS / f"{name}.png"))
        page.close()
        print(f"  saved {name}")
        return text


def wait_state(order_id, state, tries=60):
    for _ in range(tries):
        if sh(f"curl -sS {ORDER}/orders/{order_id} | jq -r .state") == state:
            return
        time.sleep(0.5)
    raise SystemExit(f"{order_id} never reached {state}")


def main():
    TRANSCRIPTS.mkdir(parents=True, exist_ok=True)
    SHOTS.mkdir(parents=True, exist_ok=True)
    print("starting a fresh system ...")
    sh("docker compose down -v --remove-orphans", check=False)
    sh("docker compose up -d --build --wait")
    time.sleep(3)

    correlation = str(uuid.uuid4())
    key = f"mt-{correlation[:8]}"

    with sync_playwright() as playwright:
        browser = playwright.chromium.launch()
        term = Terminal(browser)

        term.capture("01_compose_up", "docker compose up: four services, four stores, one JetStream server",
                     ["docker compose ps --format 'table {{.Service}}\\t{{.Status}}\\t{{.Ports}}' "
                      "| cut -c1-120"], width=1160)

        term.capture("02_order_request", "POST /orders: the only synchronous step in checkout", [
            f"curl -sS -D /tmp/h.txt -o /tmp/b.json -X POST {ORDER}/orders "
            f"-H 'content-type: application/json' -H 'x-correlation-id: {correlation}' "
            f"-H 'idempotency-key: {key}' -d '{BASKET}'; "
            "grep -Ei '^HTTP|^Location|^X-Correlation' /tmp/h.txt; jq . /tmp/b.json"],
            highlight=correlation, width=1060)
        order_id = json.loads(sh("cat /tmp/b.json"))["order_id"]
        wait_state(order_id, "CONFIRMED")

        term.capture("03_order_timeline", "GET /orders/{id}: what the order observed, with nobody directing it",
                     [f"curl -sS {ORDER}/orders/{order_id} "
                      "| jq '{state, total_cents, timeline: [.timeline[] | {state, note}]}'"],
                     highlight=order_id)

        term.capture("04_event_consumed", "notification-service: the OrderConfirmed event, consumed", [
            f"curl -sS '{NOTIFY}/notifications?order_id={order_id}' "
            "| jq '.[] | {template, subject, body, status, correlation_id}'"],
            highlight=[correlation, order_id])

        term.capture("05_correlation_logs", "one correlation ID across four services", [
            f"docker compose logs --no-log-prefix | grep {correlation} "
            "| jq -r '[.time[11:23], .service, .event] | @tsv' | sort | column -t -s $'\\t'"],
            highlight=correlation, width=1020)

        before = sh(f"curl -sS {INVENTORY}/stock | jq -r '.[]|select(.sku==\"FRIDGE-A3\")|.available'")
        cid2 = str(uuid.uuid4())
        order2 = json.loads(sh(f"curl -sS -X POST {ORDER}/orders -H 'content-type: application/json' "
                               f"-H 'x-correlation-id: {cid2}' -H 'idempotency-key: dec-{cid2[:8]}' "
                               f"-d '{BIG}'"))["order_id"]
        wait_state(order2, "CANCELLED")
        term.capture("06_compensation", "payment declined: the hold is given back, nobody orchestrating", [
            f"echo 'available before the order: {before}'",
            f"curl -sS {ORDER}/orders/{order2} | jq '{{state, failure_reason, "
            "timeline: [.timeline[] | {state, note}]}'",
            f"curl -sS {INVENTORY}/stock | jq -r '.[]|select(.sku==\"FRIDGE-A3\")"
            "|\"available after compensation: \\(.available)\"'",
            f"curl -sS '{NOTIFY}/notifications?order_id={order2}' | jq -r '.[].subject'"],
            highlight=order2, width=1020)

        print("fault isolation ...")
        cid3 = str(uuid.uuid4())
        out = term.capture("07_fault_isolation", "Notification is down: checkout is unaffected", [
            "docker compose stop notification-service 2>&1 | tail -1",
            f"curl -sS -o /dev/null -w 'checkout answered HTTP %{{http_code}} in %{{time_total}}s\\n' "
            f"-X POST {ORDER}/orders -H 'content-type: application/json' "
            f"-H 'x-correlation-id: {cid3}' -H 'idempotency-key: out-{cid3[:8]}' -d '{BASKET}'",
            f"sleep 4; {NATS} consumer report MALTASHOP 2>/dev/null "
            "| grep -E 'Consumer|notification|order-service' | cut -c1-108"], width=1060)
        assert "HTTP 201" in out, out

        term.capture("08_recovery", "Notification restarts and works through the backlog", [
            "docker compose start notification-service 2>&1 | tail -1",
            "until curl -fsS http://localhost:9183/health/ready >/dev/null 2>&1; do sleep 1; done; sleep 8; "
            f"{NATS} consumer report MALTASHOP 2>/dev/null "
            "| grep -E 'Consumer|notification|order-service' | cut -c1-108",
            f"curl -sS {NOTIFY}/notifications | jq -r '.[] | [.template, .subject] | @tsv' "
            "| head -5 | column -t -s $'\\t'"], width=1060)

        term.capture("09_jetstream", "one stream holds every event; each service reads it at its own pace", [
            f"{NATS} stream info MALTASHOP 2>/dev/null | grep -E "
            "'Subjects:|Replicas:|Retention:|Storage:|Maximum Age:|Duplicate|Messages:|Bytes:|"
            "First Sequence:|Last Sequence:|Active Consumers:' | cut -c1-92",
            f"{NATS} consumer report MALTASHOP 2>/dev/null | grep -E "
            "'Consumer .{{0,3}}│|order-service|inventory-service|payment-service|notification-service'"
            " | cut -c1-108"], width=1060)

        term.capture("10_pipeline_run", "the pipeline's verify, build and integration jobs",
                     ["./scripts/run_pipeline.sh"], width=1040)
        browser.close()

    (EVIDENCE / "demo.json").write_text(json.dumps(
        {"order_id": order_id, "correlation_id": correlation, "cancelled_order_id": order2,
         "captured_at": time.strftime("%Y-%m-%dT%H:%M:%S%z")}, indent=2) + "\n")
    print("done")


if __name__ == "__main__":
    main()
