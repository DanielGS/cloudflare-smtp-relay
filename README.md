# smtp-cloudflare-relay

A small SMTP submission server that accepts mail from applications which only speak SMTP and
delivers it through the Cloudflare Email Service HTTP API.

```text
your app  ──SMTP (plaintext, private network)──▶  smtp-cloudflare-relay
                                                          │
                                                          │ HTTPS
                                                          ▼
                                                  Cloudflare Email Service
                                                          │
                                                          ▼
                                                      recipient
```

It exists because Cloudflare's own SMTP endpoint (`smtp.mx.cloudflare.net:465`) requires a
domain onboarded to Email Sending, which is gated behind the Workers Paid plan, while the HTTP
surface can deliver to verified destination addresses for free. The relay also keeps the
Cloudflare API token in one place instead of distributing it to every application, and enforces
a sender-domain allowlist centrally.

---

## Before you start

Cloudflare-side setup is required regardless of this relay.

### 1. Enable Email Routing on the exact sending domain

A subdomain does **not** inherit Email Routing from the apex domain. If you intend to send from
`tasks.example.com`, add it explicitly:

Dashboard → **Email Routing** on `example.com` → **Settings** → **Subdomains** → add
`tasks.example.com`. Cloudflare adds the required DNS records.

This is the most common cause of `Email sending is not enabled for domain …`.

### 2. Verify your destination addresses

Dashboard → **Email Routing** → **Destination addresses**. On the free tier you can only send
to addresses verified here. Sending to arbitrary recipients requires the Workers Paid plan.

### 3. Create an API token

An API token with the **Email Sending: Edit** permission.

### 4. Probe which transport you can use

Run this once. It decides how you configure the relay.

```bash
curl -i "https://api.cloudflare.com/client/v4/accounts/$CF_ACCOUNT_ID/email/sending/send" \
  -H "Authorization: Bearer $CF_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "from": "no-reply@tasks.example.com",
    "to": "your-verified-destination@example.com",
    "subject": "relay probe",
    "text": "probe"
  }'
```

| Response | What to set |
|---|---|
| `200` with `"success": true` | `CLOUDFLARE_TRANSPORT=rest`. Nothing else needed. |
| `403` with `10105 not_entitled` or `10203 sending_disabled` | `CLOUDFLARE_TRANSPORT=worker`, and deploy the Worker in [`worker/`](worker/). |
| `403` with `10102 forbidden` | The token lacks the Email Sending permission. Fix the token. |

---

## Quick start

```bash
cp .env.example .env
# edit .env: SMTP_PASSWORD, CLOUDFLARE_ACCOUNT_ID, CLOUDFLARE_API_TOKEN, ALLOWED_FROM_DOMAINS
docker compose up --build
```

The relay listens on `2525` for SMTP and `8080` for health, on a Docker network named `mail`.
By default neither port is published to the host: containers reach it by service name.

### Connecting an application

Add your application to the same network:

```yaml
services:
  my-app:
    image: my-app:latest
    networks:
      - mail

networks:
  mail:
    external: true
```

Then point it at the relay:

```env
SMTP_HOST=smtp-cloudflare-relay
SMTP_PORT=2525
SMTP_USER=relay
SMTP_PASSWORD=change-me
EMAIL_FROM=no-reply@tasks.example.com
```

No TLS is needed on this hop: the traffic never leaves the Docker network. The relay still
requires SMTP AUTH, so an unauthenticated container cannot use it as an open relay.

---

## Configuration

Every setting comes from an environment variable. Nothing is baked into the image.

### SMTP listener

| Variable | Default | Notes |
|---|---|---|
| `SMTP_HOST` | `0.0.0.0` | |
| `SMTP_PORT` | `2525` | |
| `SMTP_USER` | *(required)* | Credentials your applications authenticate with. |
| `SMTP_PASSWORD` | *(required)* | Not Cloudflare's token. Change it. |
| `SMTP_MAX_MESSAGE_BYTES` | `5242880` | 5 MiB. Cannot be raised above Cloudflare's cap. |
| `SMTP_MAX_RECIPIENTS` | `50` | Cannot be raised above Cloudflare's cap. |
| `SMTP_READ_TIMEOUT` | `30s` | |
| `SMTP_WRITE_TIMEOUT` | `30s` | |
| `SMTP_TLS_CERT` / `SMTP_TLS_KEY` | empty | Optional. Set both to serve SMTP over TLS. |

### Cloudflare transport

| Variable | Default | Notes |
|---|---|---|
| `CLOUDFLARE_TRANSPORT` | `rest` | `rest` or `worker`. See the probe above. |
| `CLOUDFLARE_ACCOUNT_ID` | *(required for `rest`)* | |
| `CLOUDFLARE_API_TOKEN` | *(required for `rest`)* | Never logged. |
| `CLOUDFLARE_API_BASE_URL` | `https://api.cloudflare.com/client/v4` | Override for testing. |
| `CLOUDFLARE_TIMEOUT` | `15s` | Per attempt. |
| `CLOUDFLARE_MAX_RETRIES` | `2` | Temporary failures only. |
| `WORKER_URL` | *(required for `worker`)* | Your Worker's URL. |
| `WORKER_SECRET` | *(required for `worker`)* | Shared secret. Never logged. |

### Policy, health and logging

| Variable | Default | Notes |
|---|---|---|
| `ALLOWED_FROM_DOMAINS` | empty | Comma-separated. Empty means any sender. Matching is **exact**: `tasks.example.com` does not permit `example.com`. |
| `HEALTH_HOST` / `HEALTH_PORT` | `0.0.0.0` / `8080` | |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |

---

## How results are reported back to the client

The relay does not collapse every failure into `550`. It distinguishes what is worth retrying
from what is not, so a transient Cloudflare problem never causes a client to discard a valid
message.

| Situation | SMTP reply |
|---|---|
| Cloudflare accepted the message | `250 2.0.0` |
| Rate limited (`429`) | `451 4.4.5`, honoring `Retry-After` |
| Cloudflare server error (`500`, `503`) | `451 4.3.0` |
| Timeout, DNS or network failure, unreadable response | `451 4.4.1` |
| Authentication or entitlement failure (`401`, `403`) | `451 4.7.0` — see note below |
| Malformed message rejected by Cloudflare (`400`) | `550 5.6.0` |
| Account or resource not found (`404`) | `550 5.1.2` |
| Sender outside `ALLOWED_FROM_DOMAINS` | `550 5.7.1`, refused at `MAIL FROM` |
| Message above the size limit | `552 5.3.4` |
| Bad SMTP credentials | `535 5.7.8` |

**Why `401`/`403` are temporary.** A bad or unentitled token is a fault in the relay's
configuration, not in the message. Answering `550` would make the client throw away a perfectly
valid email. Answering `451` makes it retry, so the message goes out on its own once the token
is fixed. Retries are attempted only for genuinely temporary conditions, never for a permanent
rejection.

---

## Logging

One structured JSON record per message, at `info` (or `warn`/`error` when deferred or
rejected):

```json
{
  "time": "2026-09-22T07:41:12.913Z",
  "level": "INFO",
  "msg": "delivery",
  "message_id": "0b0f8f2c-9a6c-4f5c-8f5a-1d2e3f4a5b6c",
  "rfc_message_id": "20260922074112.1@app.internal",
  "from": "no-reply@tasks.example.com",
  "to": ["ops@example.com"],
  "subject": "Nightly report",
  "result": "sent",
  "duration_ms": 412,
  "http_status": 200,
  "attempts": 1
}
```

Never logged: the message body, the Cloudflare API token, the SMTP password, the Worker secret.
Subjects are truncated to 120 characters.

---

## Health

```bash
curl -s localhost:8080/health
{"status":"ok"}
```

The endpoint reports process liveness only and exposes no configuration. The container image
has no shell and no `curl`, so the Docker `HEALTHCHECK` invokes the binary's own probe mode:

```bash
docker compose exec smtp-cloudflare-relay /relay -healthcheck
```

---

## Development

```bash
make help     # list targets
make test     # go test -race -count=1 ./...
make lint     # go vet, plus golangci-lint when installed
make build    # static binary into ./bin
make run      # run locally, sourcing .env
make up       # docker compose up --build
```

Tests use doubles throughout; the Cloudflare API is simulated with `httptest`. No test makes a
live call.

---

## Verifying an end-to-end send

From a container on the `mail` network, using `swaks`:

```bash
docker run --rm --network mail instrumentisto/swaks \
  --server smtp-cloudflare-relay:2525 \
  --auth PLAIN --auth-user relay --auth-password change-me \
  --from no-reply@tasks.example.com \
  --to your-verified-destination@example.com \
  --header "Subject: relay test" \
  --body "hello from the relay"
```

Expect `250` and one log line with `"result":"sent"`.

Check the rejection paths too:

```bash
# wrong password           -> 535
# sender on another domain -> 550
# attachment over 5 MiB    -> 552
```

---

## Troubleshooting

**`550 5.7.1 Email sending is not enabled for domain …`**
Cloudflare is rejecting the *sender domain*. Confirm that the exact domain or subdomain is
added under Email Routing → Settings → Subdomains, and that the probe in step 4 succeeds.

**`403` with `10105 not_entitled` from the probe**
Your account cannot use the Email Sending REST surface. Switch to
`CLOUDFLARE_TRANSPORT=worker`, or buy the Workers Paid plan.

**Mail accepted by the relay but never delivered**
On the free tier the recipient must be a verified destination address. Check Email Routing →
Destination addresses.

**Client reports `451` repeatedly**
Read the logs: `http_status` and `cf_error_code` name the upstream cause. A `401`/`403` there
means the token is wrong or lacks `Email Sending: Edit`.

---

## Scope

Not included, by design: mail reception, IMAP/POP3, a persistent queue, unbounded retries, a
web panel, a database.

## License

MIT
