#!/usr/bin/env python3
"""Draws every figure in the report as an SVG in diagrams/src/.

render.sh turns them into print-resolution PNGs in diagrams/out/.
"""
from svgkit import (OUT, svg, text, text_lines, rect, box, arrow, polyline, line, cylinder, service,
                    INK, GREY, LIGHT, WHITE, ACCENT, AMBER, AMBER_FILL, BLUE, BLUE_FILL, OLIVE, OLIVE_FILL)

W = 880


def write(name, width, height, body):
    (OUT / f"{name}.svg").write_text(svg(width, height, body))
    print(f"  {name}.svg  {width}x{height}")


def band(x, y, w, h, title, subtitle, stroke=INK, fill=LIGHT, title_size=14.5):
    out = [rect(x, y, w, h, fill, stroke, 1.6)]
    out.append(text(x + w / 2, y + 24, title, size=title_size, bold=True, colour=stroke))
    if subtitle:
        out.append(text(x + w / 2, y + 43, subtitle, size=11.5, colour=GREY))
    return "\n".join(out)


# ------------------------------------------------------------------ Figure 1
def architecture():
    h = 600
    p = [rect(0, 0, W, h, WHITE, WHITE, 0)]

    for i, (label, sub) in enumerate((("Web storefront", "browser"), ("Mobile app", "iOS / Android"),
                                      ("30 partner retailers", "server to server"))):
        x = 24 + i * 290
        p.append(box(x, 12, 262, 44, label, [sub], fill=WHITE, title_size=13.5, size=11.5))
        p.append(arrow(x + 131, 56, 160 + i * 280, 84, "ink"))

    p.append(band(24, 84, 832, 56, "Edge gateway", "TLS · routing · per-retailer API key and scope · "
                                                   "rate limit 60 req/min per partner · customer sessions",
                  stroke=BLUE, fill=BLUE_FILL, title_size=15))

    # extracted services
    p.append(rect(24, 172, 500, 122, "#fdf2f5", ACCENT, 1.5, dashed=True))
    p.append(text(274, 191, "extracted first — the hot path, and only the hot path", size=12,
                  italic=True, colour=ACCENT))
    for i, (name, store) in enumerate((("Order", "orders"), ("Inventory", "stock · holds"), ("Payment", "payments"))):
        p.append(service(36 + i * 164, 200, 150, 82, name, [store], critical=True))
    p.append(arrow(274, 140, 274, 170, "ink"))

    # the legacy core
    p.append(rect(552, 164, 304, 130, OLIVE_FILL, OLIVE, 1.8))
    p.append(text(704, 188, "Legacy core (modular monolith)", size=14, bold=True, colour=OLIVE))
    p.append(text_lines(704, 208, ["Catalogue · Customer · Reporting", "still PHP, still one MySQL schema,",
                                   "now behind an anti-corruption layer", "and shrinking one module at a time"],
                        size=11.5, gap=16))
    p.append(cylinder(566, 258, colour=OLIVE))
    p.append(text(704, 288, "MySQL", size=11.5, colour=OLIVE))
    p.append(arrow(704, 140, 704, 162, "ink"))
    p.append(arrow(524, 232, 550, 232, "ink", both=True))
    p.append(text(537, 222, "ACL", size=10.5, colour=GREY))

    # messaging
    p.append(band(24, 318, 832, 56, "NATS JetStream — one stream, MALTASHOP, subject maltashop.>",
                  "eight subjects · 30-day file retention · one durable pull consumer per service · "
                  "2-minute duplicate window", stroke=AMBER, fill=AMBER_FILL, title_size=15))
    for x in (111, 275, 439, 704):
        p.append(arrow(x, 294, x, 316, "amber", both=True))

    # consumers
    p.append(rect(24, 400, 256, 104, WHITE, INK, 1.4))
    p.append(text(152, 422, "Notification", size=14, bold=True))
    p.append(cylinder(96, 434))
    p.append(text(124, 450, "Valkey keyspace", size=11.5, anchor="start"))
    p.append(text_lines(152, 474, ["e-mail and SMS; if it dies the",
                                   "events simply wait on the stream"], size=11.5, gap=15))
    p.append(arrow(152, 374, 152, 398, "amber"))

    p.append(rect(304, 400, 256, 104, OLIVE_FILL, OLIVE, 1.5))
    p.append(text(432, 422, "AI Recommendation", size=14, bold=True, colour=OLIVE))
    p.append(text_lines(432, 441, ["Python; reads order and browsing",
                                   "events from the stream, trains and",
                                   "serves through the gateway, never",
                                   "on the checkout path"], size=11.5, gap=15))
    p.append(arrow(432, 374, 432, 398, "olive"))

    p.append(rect(584, 400, 272, 104, BLUE_FILL, BLUE, 1.5))
    p.append(text(720, 422, "Observability", size=14, bold=True, colour=BLUE))
    p.append(text_lines(720, 441, ["OpenTelemetry SDKs → Collector →",
                                   "SigNoz (self-hosted, ClickHouse)",
                                   "logs, metrics and traces in one place,",
                                   "joined by correlation_id"], size=11.5, gap=15))
    p.append(polyline([(856, 346), (868, 346), (868, 452), (854, 452)], "blue", dashed=True))

    p.append(rect(24, 524, 832, 60, WHITE, GREY, 1.2, dashed=True))
    p.append(text(440, 546, "Runtime: AWS ECS Fargate for the first two quarters, then EKS in phase three. "
                            "Managed PostgreSQL (RDS),", size=12.5, bold=True, colour=GREY))
    p.append(text(440, 564, "managed Valkey and a three-node NATS cluster sit outside the task runtime. "
                            "Plain arrow = synchronous · double arrow = publish and subscribe",
                  size=11.5, italic=True, colour=GREY))
    write("architecture", W, h, "\n".join(p))


# ------------------------------------------------------------------ Figure 2
def pipeline():
    h = 486
    p = [rect(0, 0, W, h, WHITE, WHITE, 0)]
    p.append(box(16, 44, 118, 54, "git push", ["main, or a", "one-day branch"], fill=WHITE, title_size=13.5, size=11))

    # verify: two jobs that run at the same time
    p.append(box(170, 12, 168, 56, "lint", ["gofmt · golangci-lint"], fill=LIGHT, title_size=13, size=11))
    p.append(box(170, 84, 168, 66, "test", ["go test ./... -race", "unit + contract", "coverage gate 60 %"],
                 fill=LIGHT, title_size=13, size=11))
    p.append(arrow(134, 62, 168, 40, "ink"))
    p.append(arrow(134, 76, 168, 112, "ink"))

    p.append(box(372, 42, 172, 72, "build", ["one Dockerfile,", "four images,", "tagged by commit SHA"],
                 fill=BLUE_FILL, stroke=BLUE, title_size=13, size=11))
    p.append(arrow(338, 40, 370, 66, "ink"))
    p.append(arrow(338, 117, 370, 90, "ink"))

    p.append(box(578, 10, 160, 60, "integration", ["compose up --wait", "the whole saga"],
                 fill=BLUE_FILL, stroke=BLUE, title_size=13, size=11))
    p.append(box(578, 86, 160, 72, "scan", ["Trivy: HIGH/CRITICAL", "gitleaks: secrets",
                                            "both block the release"], fill="#fdf2f5", stroke=ACCENT,
                 title_size=13, size=11))
    p.append(arrow(544, 66, 576, 44, "ink"))
    p.append(arrow(544, 90, 576, 112, "ink"))

    p.append(text(440, 186, "the four images built once are the only ones ever deployed; later jobs name them by digest",
                  size=12, italic=True, colour=ACCENT))

    p.append(rect(60, 212, 368, 96, OLIVE_FILL, OLIVE, 1.6))
    p.append(text(244, 236, "release: order, inventory, notification", size=13.5, bold=True, colour=OLIVE))
    p.append(text_lines(244, 256, ["continuous deployment — no human in the way",
                                   "rolling update: new tasks must pass readiness",
                                   "before any old task is drained",
                                   "ten-minute watch, automatic roll-back above 1 % 5xx"],
                        size=11.5, gap=15))
    p.append(polyline([(658, 158), (658, 176), (244, 176), (244, 210)], "ink"))

    p.append(rect(468, 212, 352, 96, "#fdf2f5", ACCENT, 1.6))
    p.append(text(644, 236, "release: payment — manual", size=13.5, bold=True, colour=ACCENT))
    p.append(text_lines(644, 256, ["the one job a person still starts, because a bad",
                                   "payment release costs money, not minutes",
                                   "same rolling update, tighter guard (0.5 % over 15 min)",
                                   "runs only after the other three are stable"],
                        size=11.5, gap=15))
    p.append(arrow(428, 260, 466, 260, "accent"))

    p.append(rect(60, 334, 760, 90, WHITE, GREY, 1.2, dashed=True))
    p.append(text(440, 356, "a rolling update, in detail", size=13, bold=True, colour=GREY))
    for i, (label, sub, fill, stroke) in enumerate((("4 old", "version n", LIGHT, INK),
                                                    ("3 old + 2 new", "both serving", BLUE_FILL, BLUE),
                                                    ("1 old + 4 new", "draining", BLUE_FILL, BLUE),
                                                    ("4 new", "version n+1", OLIVE_FILL, OLIVE))):
        x = 92 + i * 182
        p.append(box(x, 372, 150, 42, label, [sub], fill=fill, stroke=stroke, title_size=12.5, size=10.5))
        if i:
            p.append(arrow(x - 28, 393, x - 4, 393, "grey"))

    p.append(text(440, 452, "no second environment and no traffic splitting: capacity never dips, "
                            "and nothing is paid for twice", size=12, italic=True, colour=GREY))
    p.append(text(440, 472, "roll back = redeploy the previous task definition, which is still registered "
                            "(about 90 seconds)", size=12, italic=True, colour=GREY))
    write("pipeline", W, h, "\n".join(p))


# ------------------------------------------------------------------ Figure 3
def observability():
    h = 456
    p = [rect(0, 0, W, h, WHITE, WHITE, 0)]
    for i, name in enumerate(("Order", "Inventory", "Payment", "Notification")):
        p.append(box(24 + i * 124, 18, 108, 58, name, ["OTel SDK"], fill=WHITE, title_size=13, size=11))
        p.append(arrow(78 + i * 124, 76, 300, 110, "ink"))
    p.append(box(540, 18, 316, 58, "Legacy core and the gateway",
                 ["structured logs and basic metrics — no spans yet"], fill=OLIVE_FILL, stroke=OLIVE,
                 title_size=13, size=11))
    p.append(arrow(698, 76, 540, 110, "olive"))

    p.append(band(176, 110, 528, 50, "OpenTelemetry Collector",
                  "batches · samples · strips names, addresses and card data before anything leaves the cluster",
                  stroke=BLUE, fill=BLUE_FILL))

    p.append(rect(176, 190, 528, 62, LIGHT, INK, 1.6))
    p.append(text(440, 214, "SigNoz — self-hosted, ClickHouse-backed, OpenTelemetry-native",
                  size=14.5, bold=True))
    p.append(text(440, 234, "logs 30 days · metrics 13 months · traces 15 days — all three joined by correlation_id",
                  size=11.5, colour=GREY))
    p.append(arrow(440, 160, 440, 188, "blue"))

    p.append(rect(24, 286, 404, 130, "#fdf2f5", ACCENT, 1.5))
    p.append(text(226, 308, "SLO: checkout", size=14, bold=True, colour=ACCENT))
    p.append(text_lines(226, 328, ["99.9 % of POST /orders answer 2xx in ≤ 900 ms,",
                                   "measured over a rolling 30 days",
                                   "error budget: 43 min 12 s a month"], size=11.5, gap=16))
    p.append(line(40, 382, 412, 382, colour=ACCENT, width=1.0, dashed=True))
    p.append(text_lines(226, 398, ["alert 1  burn 14.4× over 5 min → severity 1, page",
                                   "alert 2  burn 6× over 1 h → severity 2, Slack"], size=11, gap=15))

    p.append(rect(452, 286, 404, 130, WHITE, INK, 1.4))
    p.append(text(654, 308, "what the March incident would have looked like",
                  size=13.5, bold=True))
    p.append(text_lines(654, 328, ["1  memory of notification-service climbs; alert 2 fires",
                                   "2  the service restarts; checkout is untouched",
                                   "3  the engineer filters by service, not by guesswork",
                                   "4  one correlation ID shows the order completed anyway",
                                   "5  the unsent e-mails are still on the stream"],
                        size=11, gap=15.5))
    p.append(arrow(300, 252, 226, 284, "ink"))
    p.append(arrow(580, 252, 654, 284, "ink"))
    write("observability", W, h, "\n".join(p))


# ------------------------------------------------------------------ Figure 4
def saga():
    h = 580
    p = [rect(0, 0, W, h, WHITE, WHITE, 0)]
    p.append(text(440, 24, "Nobody is in charge: each service reacts to what happened and publishes what it decided.",
                  size=12.5, italic=True, colour=GREY))

    def node(x, y, w, title, rows, stroke=INK, fill=WHITE):
        out = [rect(x, y, w, 56, fill, stroke, 1.5),
               text(x + w / 2, y + 23, title, size=12, bold=True, colour=stroke)]
        out.append(text_lines(x + w / 2, y + 40, rows, size=10, gap=13, colour=GREY))
        return "\n".join(out)

    def hop(x1, y, x2, label, size=9.8):
        return "\n".join([arrow(x1, y, x2, y, "amber"),
                           text((x1 + x2) / 2, y - 8, label, size=size, colour=AMBER)])

    # --- happy path --------------------------------------------------------
    p.append(text(24, 62, "the order completes", size=12.5, bold=True, anchor="start"))
    p.append(node(24, 74, 120, "Order", ["writes the order,", "answers 201"], ACCENT))
    p.append(hop(146, 102, 250, "order.placed"))
    p.append(node(252, 74, 120, "Inventory", ["holds the stock", "(try)"], ACCENT))
    p.append(hop(374, 102, 478, "inventory.reserved"))
    p.append(node(480, 74, 120, "Payment", ["asks the card", "provider"], ACCENT))
    p.append(hop(602, 102, 706, "payment.authorised"))
    p.append(node(708, 74, 148, "Order → CONFIRMED", ["publishes order.confirmed"], ACCENT))

    p.append(polyline([(782, 130), (782, 152), (312, 152), (312, 172)], "amber"))
    p.append(text(548, 146, "order.confirmed", size=9.8, colour=AMBER))
    p.append(node(252, 172, 120, "Inventory", ["deducts the stock", "(confirm)"], ACCENT))
    p.append(polyline([(782, 130), (782, 172)], "amber"))
    p.append(node(708, 172, 148, "Notification", ["records the e-mail"], INK))

    # --- compensation ------------------------------------------------------
    p.append(rect(24, 246, 832, 128, "#fdf2f5", ACCENT, 1.4, dashed=True))
    p.append(text(40, 268, "the card is declined — the saga is undone, step by step",
                  size=12.5, bold=True, anchor="start", colour=ACCENT))
    p.append(node(40, 284, 128, "Payment", ["declines, publishes"], ACCENT, WHITE))
    p.append(hop(170, 312, 262, "payment.declined"))
    p.append(node(264, 284, 128, "Inventory", ["hands the hold back", "(cancel)"], ACCENT, WHITE))
    p.append(hop(394, 312, 486, "inventory.released"))
    p.append(node(488, 284, 128, "Order → CANCELLED", ["publishes order.cancelled"], ACCENT, WHITE))
    p.append(hop(618, 312, 710, "order.cancelled"))
    p.append(node(712, 284, 128, "Notification", ["tells the customer"], INK, WHITE))
    p.append(text(440, 360, "nothing is rolled back: every undo is an ordinary, idempotent operation "
                            "that is itself an event", size=11, italic=True, colour=GREY))

    # --- the stream --------------------------------------------------------
    p.append(rect(24, 392, 832, 46, AMBER_FILL, AMBER, 1.7))
    p.append(text(440, 414, "every arrow above is one message on NATS JetStream, stream MALTASHOP, "
                            "subject maltashop.>", size=13, bold=True, colour=AMBER))
    p.append(text(440, 431, "no service calls another service; each has one durable consumer and reads "
                            "at its own pace", size=11, colour=GREY))

    p.append(rect(24, 458, 404, 110, WHITE, INK, 1.3))
    p.append(text(226, 480, "why the steps still happen in order", size=13, bold=True))
    p.append(text_lines(226, 499, ["Payment subscribes to inventory.reserved and never",
                                   "to order.placed, so payment cannot be taken for",
                                   "stock that was never held. The sequence is a",
                                   "property of who subscribes to what."], size=11, gap=16))

    p.append(rect(452, 458, 404, 110, OLIVE_FILL, OLIVE, 1.3))
    p.append(text(654, 480, "why a repeat changes nothing", size=13, bold=True, colour=OLIVE))
    p.append(text_lines(654, 499, ["A publish carries the event ID as Nats-Msg-Id, which",
                                   "JetStream drops inside a two-minute window. Each",
                                   "consumer then claims the event ID once in its own",
                                   "store, and an event that does not fit the current",
                                   "state is logged and dropped."], size=11, gap=15))
    write("saga", W, h, "\n".join(p))


# ------------------------------------------------------------------ Figure 5
def prototype():
    h = 430
    p = [rect(0, 0, W, h, WHITE, WHITE, 0)]
    p.append(rect(16, 12, 848, 404, WHITE, GREY, 1.3, dashed=True))
    p.append(text(440, 34, "docker compose up --build -d --wait   —   one command, nine containers",
                  size=14, bold=True, colour=GREY))

    cells = [("order-service", ":9180", "PostgreSQL 16", True),
             ("inventory-service", ":9181", "PostgreSQL 16", True),
             ("payment-service", ":9182", "PostgreSQL 16", True),
             ("notification-service", ":9183", "Valkey 8", False)]
    for i, (name, port, store, critical) in enumerate(cells):
        x = 32 + i * 208
        stroke = ACCENT if critical else INK
        p.append(rect(x, 54, 192, 68, WHITE, stroke, 1.6))
        p.append(text(x + 96, 76, name, size=12.5, bold=True, colour=stroke))
        p.append(text(x + 96, 94, port + "   ·   Go 1.23", size=11.5, colour=GREY))
        p.append(text(x + 96, 112, "distroless, non-root", size=10.5, italic=True, colour=GREY))
        p.append(arrow(x + 50, 122, x + 50, 150, "ink", both=True))
        p.append(rect(x + 4, 150, 116, 40, LIGHT, INK, 1.1))
        p.append(cylinder(x + 14, 159))
        p.append(text(x + 78, 175, store, size=11))
        p.append(polyline([(x + 150, 122), (x + 150, 222)], "amber", end_arrow=True))
        p.append(polyline([(x + 158, 222), (x + 158, 122)], "amber", end_arrow=True))

    p.append(rect(32, 222, 800, 52, AMBER_FILL, AMBER, 1.7))
    p.append(text(432, 245, "NATS JetStream — one server, one stream, four durable consumers",
                  size=14, bold=True, colour=AMBER))
    p.append(text(432, 263, "client :4322 · monitoring :8322 · file storage, 30-day retention",
                  size=11, colour=GREY))

    p.append(rect(32, 298, 390, 104, OLIVE_FILL, OLIVE, 1.4))
    p.append(text(227, 320, "what the prototype proves", size=13, bold=True, colour=OLIVE))
    p.append(text_lines(227, 339, ["checkout answers in ~5 ms and still answers 201",
                                   "while Notification is stopped",
                                   "the hold is released when payment is declined",
                                   "one correlation ID runs through all four services"],
                        size=11, gap=15))

    p.append(rect(442, 298, 390, 104, "#fdf2f5", ACCENT, 1.4))
    p.append(text(637, 320, "what it does not prove", size=13, bold=True, colour=ACCENT))
    p.append(text_lines(637, 339, ["one NATS node, replica count 1",
                                   "no gateway, no API keys, no legacy core",
                                   "a stubbed card provider",
                                   "no load test: 5 ms is one request, not peak traffic"],
                        size=11, gap=15))
    write("prototype", W, h, "\n".join(p))


if __name__ == "__main__":
    OUT.mkdir(parents=True, exist_ok=True)
    architecture()
    pipeline()
    observability()
    saga()
    prototype()
