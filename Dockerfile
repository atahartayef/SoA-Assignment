# syntax=docker/dockerfile:1.7
# One Dockerfile builds all four services. SERVICE picks which main package to
# compile, so there is a single file to review and patch rather than four copies
# that drift apart - the lean choice that matches a fourteen-person platform team.

# ---------- stage 1: compile a static binary ---------------------------------
FROM golang:1.23.3-alpine3.20 AS build
ARG SERVICE
WORKDIR /src
# Dependencies are their own layer, so a code change does not re-download them.
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY cmd ./cmd
# CGO off produces a statically linked binary that needs no libc in the final
# image. -trimpath and -ldflags "-s -w" strip paths and symbols: a smaller image
# and nothing about the build machine left inside it.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/service ./cmd/${SERVICE}

# ---------- stage 2: the image that ships ------------------------------------
# Distroless holds the binary, CA certificates and time-zone data - no shell, no
# package manager, no busybox. There is nothing for an attacker to run.
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
ARG SERVICE
ARG PORT=8080
ENV SERVICE_NAME=${SERVICE}-service \
    PORT=${PORT}
COPY --from=build /out/service /app/service
# nonroot (uid 65532) is the image's default user; it is named here so that a
# later edit cannot silently drop back to root.
USER 65532:65532
EXPOSE ${PORT}
# No curl exists in this image, so the binary probes itself.
HEALTHCHECK --interval=15s --timeout=4s --start-period=12s --retries=3 \
  CMD ["/app/service", "-healthcheck"]
ENTRYPOINT ["/app/service"]
