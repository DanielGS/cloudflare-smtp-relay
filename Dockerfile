# syntax=docker/dockerfile:1

# ---- build stage ------------------------------------------------------------
FROM golang:1.27-alpine AS build

WORKDIR /src

# Dependencies first, so edits to the source do not invalidate the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is disabled so the binary is fully static and can run on a scratch-like base.
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/relay \
        ./cmd/relay

# ---- runtime stage ----------------------------------------------------------
# static-debian12:nonroot ships CA certificates (required for HTTPS to Cloudflare)
# and a preconfigured non-root user, with no shell and no package manager.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/relay /relay

# 2525 is SMTP submission, 8080 serves GET /health.
EXPOSE 2525 8080

USER nonroot:nonroot

# The image has no shell and no curl, so the binary probes itself.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/relay", "-healthcheck"]

ENTRYPOINT ["/relay"]
