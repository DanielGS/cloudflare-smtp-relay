# Operations

## How the relay answers your client

The relay does not collapse every failure into `550`. It separates what is worth retrying from
what is not, so a transient Cloudflare problem never makes a client discard a valid message.

| Situation | SMTP reply |
|---|---|
| ✅ Cloudflare accepted the message | `250 2.0.0` |
| ⏳ Rate limited (`429`) | `451 4.4.5`, honoring `Retry-After` |
| ⏳ Cloudflare server error (`500`, `503`) | `451 4.3.0` |
| ⏳ Timeout, DNS or network failure, unreadable response | `451 4.4.1` |
| ⏳ Authentication or entitlement failure (`401`, `403`) | `451 4.7.0` — see below |
| ❌ Malformed message rejected by Cloudflare (`400`) | `550 5.6.0` |
| ❌ Account or resource not found (`404`) | `550 5.1.2` |
| ❌ Sender outside `ALLOWED_FROM_DOMAINS` | `550 5.7.1`, refused at `MAIL FROM` |
| ❌ Message above the size limit | `552 5.3.4` |
| ❌ Bad SMTP credentials | `535 5.7.8` |

> **Why `401`/`403` are temporary.** A bad or unentitled token is a fault in the relay's
> configuration, not in the message. Answering `550` would make the client throw away a
> perfectly valid email. Answering `451` makes it retry, so the message goes out on its own
> once the token is fixed. Retries are attempted only for genuinely temporary conditions,
> never for a permanent rejection.

---

## Logging

One structured JSON record per message, at `info` — or `warn`/`error` when deferred or
rejected:

```json
{
  "time": "2026-09-22T07:41:12.913Z",
  "level": "INFO",
  "msg": "delivery",
  "message_id": "0b0f8f2c-9a6c-4f5c-8f5a-1d2e3f4a5b6c",
  "rfc_message_id": "20260922074112.1@app.internal",
  "from": "no-reply@subdomain.mydomainexample.com",
  "to": ["ops@example.com"],
  "subject": "Nightly report",
  "result": "sent",
  "duration_ms": 412,
  "http_status": 200,
  "attempts": 1
}
```

**Never logged:** the message body, the Cloudflare API token, the SMTP password, the Worker
secret. Subjects are truncated to 120 characters.

---

## Health

```bash
curl -s localhost:8080/health
{"status":"ok"}
```

The endpoint reports process liveness only and exposes no configuration. The container image
has no shell and no `curl`, so the Docker `HEALTHCHECK` invokes the binary's own probe mode:

```bash
docker compose exec cloudflare-smtp-relay /relay -healthcheck
```

---

## Verifying an end-to-end send

From a container on the `mail` network, using `swaks`:

```bash
docker run --rm --network mail instrumentisto/swaks \
  --server cloudflare-smtp-relay:2525 \
  --auth PLAIN --auth-user relay --auth-password change-me \
  --from no-reply@subdomain.mydomainexample.com \
  --to your-verified-destination@example.com \
  --header "Subject: relay test" \
  --body "hello from the relay"
```

Expect `250` and one log line with `"result":"sent"`.

Worth checking the rejection paths too:

- [ ] Wrong password → `535`
- [ ] Sender on another domain → `550`
- [ ] Attachment over 5 MiB → `552`

---

## Troubleshooting

<details>
<summary><code>550 5.7.1 Email sending is not enabled for domain …</code></summary>

Cloudflare is rejecting the **sender domain**. Confirm the exact domain or subdomain is added
under Email Routing → Settings → Subdomains, and that
[the probe](cloudflare-setup.md#4-probe-which-transport-your-account-can-use) succeeds.

</details>

<details>
<summary><code>403</code> with <code>10105 not_entitled</code> from the probe</summary>

Your account cannot use the Email Sending REST surface. Switch to
`CLOUDFLARE_TRANSPORT=worker`, or buy the Workers Paid plan.

</details>

<details>
<summary>Mail accepted by the relay but never delivered</summary>

On the free tier the recipient must be a verified destination address. Check Email Routing →
Destination addresses.

</details>

<details>
<summary>Client reports <code>451</code> repeatedly</summary>

Read the logs: `http_status` and `cf_error_code` name the upstream cause. A `401`/`403` there
means the token is wrong or lacks `Email Sending: Edit`.

</details>
