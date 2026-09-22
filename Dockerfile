# syntax=docker/dockerfile:1

# ---- build stage ------------------------------------------------------------
# --platform=$BUILDPLATFORM pins the build stage to the host/builder platform so
# buildx runs it natively for every target, instead of emulating it under QEMU.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build

# TARGETOS/TARGETARCH are set by buildx to the platform being built for
# (e.g. linux/arm64), independently of the native build platform above.
ARG TARGETOS
ARG TARGETARCH

# VERSION is injected by the caller (the git tag in CI, "dev" locally) and
# stamped into the binary since .dockerignore excludes .git, which means Go's
# automatic VCS stamping produces nothing inside the image.
ARG VERSION=dev

WORKDIR /src

# Dependencies first, so edits to the source do not invalidate the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is disabled so the binary is fully static and can run on a scratch-like base.
# GOOS/GOARCH cross-compile natively for the target platform, so no QEMU is needed.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
        -trimpath \
        -ldflags="-s -w -X main.version=$VERSION" \
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
