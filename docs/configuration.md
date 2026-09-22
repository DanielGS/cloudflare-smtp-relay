# Configuration

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
| `CLOUDFLARE_TRANSPORT` | `rest` | `rest` or `worker`. See [the probe](cloudflare-setup.md#4-probe-which-transport-your-account-can-use). |
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
| `ALLOWED_FROM_DOMAINS` | empty | Comma-separated; spaces, case and duplicates are normalized away. Empty means any sender. Matching is **exact**, with no wildcards and no subdomain inheritance: `mydomainexample.com` does not permit `notifications.mydomainexample.com`. List every domain. |
| `HEALTH_HOST` / `HEALTH_PORT` | `0.0.0.0` / `8080` | |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
