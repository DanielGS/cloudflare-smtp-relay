# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- A manually-triggered `release.yml` workflow that rolls the `[Unreleased]` changelog entries
  into a new dated version section, tags the release, publishes the GitHub release, and
  dispatches `docker.yml` at the release tag to build and push the versioned image.

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

[Unreleased]: https://github.com/DanielGS/cloudflare-smtp-relay/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/DanielGS/cloudflare-smtp-relay/releases/tag/v1.0.0
