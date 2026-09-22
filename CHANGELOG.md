# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- README split into a short pitch/quick-start plus focused reference docs:
  `CONTRIBUTING.md`, `SECURITY.md`, `docs/cloudflare-setup.md`,
  `docs/configuration.md`, `docs/operations.md`.
- Quick start now fetches `examples/docker-compose.yml` with `curl` and runs
  the published image directly, instead of cloning the repository and
  building from source.

## [1.0.1] - 2026-09-22

### Added

- A manually-triggered `release.yml` workflow that rolls the `[Unreleased]` changelog entries
  into a new dated version section, tags the release, publishes the GitHub release, and
  dispatches `docker.yml` at the release tag to build and push the versioned image. It
  refuses to run unless the version is bare semver, the tag is new, `[Unreleased]` has
  content, and CI concluded `success` for the exact commit being released.

### Changed

- README now leads with the free-tier value proposition: sending through Email Routing on any
  plan, and why Cloudflare's own SMTP endpoint cannot reach that path.
- `ALLOWED_FROM_DOMAINS` is documented with a multi-domain example, the normalization it
  applies, and why matching is exact rather than by suffix.

## [1.0.0] - 2026-09-22

### Added

- SMTP submission server that requires SMTP AUTH on every connection, so the relay cannot be
  used as an open relay.
- Sender-domain allowlist enforced at `MAIL FROM`, rejecting messages from senders outside
  `ALLOWED_FROM_DOMAINS` before they reach Cloudflare.
- Two Cloudflare Email Service transports: the Email Sending REST API, and a Worker transport
  that talks to a user-deployed Worker holding the `send_email` binding for accounts without
  REST entitlement.
- SMTP reply-code classification that separates temporary failures (rate limits, server errors,
  timeouts, authentication/entitlement faults) from permanent ones (malformed messages, unknown
  resources, oversized messages, bad credentials), so a transient Cloudflare problem never makes
  a client discard a valid message.
- Bounded retries with jittered backoff for temporary failures, honoring the upstream
  `Retry-After` header.
- Structured JSON delivery logs, one record per message, that never contain the message body,
  the Cloudflare API token, the SMTP password, or the Worker secret.
- A liveness endpoint (`GET /health`) that reports process liveness only, plus a shell-free
  container self-probe (`relay -healthcheck`) for the distroless image, which has no shell and
  no `curl`.
- A multi-arch (`linux/amd64`, `linux/arm64`) container image published to GitHub Container
  Registry on every push to `main` and on tagged releases.
- CI running the test suite and linting on every push and pull request.
- Build-time version stamping: the version is injected through `-ldflags`, reported by
  `relay -version`, and included in the `relay starting` log line.

[Unreleased]: https://github.com/DanielGS/cloudflare-smtp-relay/compare/v1.0.1...HEAD
[1.0.1]: https://github.com/DanielGS/cloudflare-smtp-relay/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/DanielGS/cloudflare-smtp-relay/releases/tag/v1.0.0
