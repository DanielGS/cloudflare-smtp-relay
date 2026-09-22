# Worker transport

Deploy this Worker **only if** the REST probe in the root README returns `403` with
`10105 not_entitled` or `10203 sending_disabled`. If the probe succeeds, use
`CLOUDFLARE_TRANSPORT=rest` and ignore this directory entirely.

## Why it exists

The Email Sending REST API lives behind the Workers Paid plan for some accounts. The Email
Routing `send_email` binding does not: it can deliver to addresses already verified as
destination addresses on any plan. This Worker exposes that binding over HTTP so the relay,
which runs outside Cloudflare, can reach it.

The trade is the one you accepted by staying on the free tier: **recipients must be verified
destination addresses**. Sending to an arbitrary address requires the Workers Paid plan.

## Deploy

```bash
cd worker
npx wrangler deploy
npx wrangler secret put RELAY_SECRET     # paste a long random string
```

Generate the secret with something like `openssl rand -hex 32`. It is a shared secret between
the relay and this Worker; it is not a Cloudflare credential.

Then configure the relay:

```env
CLOUDFLARE_TRANSPORT=worker
WORKER_URL=https://smtp-relay-sender.<your-subdomain>.workers.dev
WORKER_SECRET=<the same random string>
```

`CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_API_TOKEN` are unused in this mode — the Worker
authenticates to Email Routing through its binding, not through a token.

## Contract

`POST /` with `Authorization: Bearer <RELAY_SECRET>` and a JSON body in the same shape the
relay sends to the REST API:

```json
{
  "from": { "address": "no-reply@subdomain.mydomainexample.com", "name": "Notifications" },
  "to":   [{ "address": "ops@example.com" }],
  "cc":   [],
  "bcc":  [],
  "reply_to": "support@mydomainexample.com",
  "subject": "Nightly report",
  "text": "plain text body",
  "html": "<p>html body</p>",
  "headers": { "X-Source": "relay" }
}
```

The Worker translates `address` to the binding's `email` key and `reply_to` to `replyTo`; that
naming difference between the two Cloudflare surfaces is handled here so the relay only ever
speaks one shape.

Responses:

| Status | Body | Meaning |
|---|---|---|
| `200` | `{"success":true,"messageId":"..."}` | Accepted |
| `400` | `{"success":false,"code":"E_INVALID_REQUEST",...}` | Malformed payload |
| `401` | `{"success":false,"code":"E_UNAUTHORIZED",...}` | Wrong shared secret |
| `403` | `{"success":false,"code":"E_SENDER_NOT_VERIFIED",...}` | Sender or recipient rejected |
| `413` | `{"success":false,"code":"E_TOO_BIG",...}` | Over 5 MiB |
| `429` | `{"success":false,"code":"E_RATE_LIMIT_EXCEEDED",...}` | Rate limited |
| `502` | `{"success":false,"code":"E_SEND_FAILED",...}` | Binding failed |

The relay classifies these into SMTP reply codes; see the table in the root README.

## Security notes

- The bearer token is compared in constant time, so a wrong secret leaks neither its length nor
  a matching prefix through response timing.
- `RELAY_SECRET` is an encrypted Wrangler secret, deliberately not declared in
  `wrangler.jsonc`, so it never lands in the repository.
- The Worker refuses to start handling requests if `RELAY_SECRET` is unset, rather than
  defaulting to open access.
- Narrow the blast radius further with `allowed_destination_addresses` or
  `allowed_sender_addresses` in `wrangler.jsonc` if you know the exact addresses in play.
