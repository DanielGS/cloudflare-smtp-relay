/**
 * Cloudflare Worker that fronts the Email Routing `send_email` binding.
 *
 * The relay uses this only when the Email Sending REST API is not available to
 * the account. The binding can deliver to verified destination addresses on any
 * plan, which is the free path the REST surface may be gated away from.
 *
 * The relay speaks the REST wire shape (`address` inside recipient objects,
 * `reply_to` in snake_case). The binding expects `email` and `replyTo`, so the
 * translation happens here and nowhere else.
 */

/** Maximum accepted request body, mirroring Cloudflare's 5 MiB message cap. */
const MAX_BODY_BYTES = 5 * 1024 * 1024;

export default {
  async fetch(request, env) {
    if (request.method !== "POST") {
      return json(405, { success: false, code: "E_METHOD_NOT_ALLOWED", error: "Use POST" });
    }

    if (!env.RELAY_SECRET) {
      return json(500, {
        success: false,
        code: "E_NOT_CONFIGURED",
        error: "RELAY_SECRET is not set on the Worker",
      });
    }

    if (!authorized(request, env.RELAY_SECRET)) {
      return json(401, { success: false, code: "E_UNAUTHORIZED", error: "Invalid credentials" });
    }

    const declared = Number(request.headers.get("content-length") || "0");
    if (declared > MAX_BODY_BYTES) {
      return json(413, { success: false, code: "E_TOO_BIG", error: "Message exceeds 5 MiB" });
    }

    let payload;
    try {
      payload = await request.json();
    } catch {
      return json(400, { success: false, code: "E_BAD_JSON", error: "Body is not valid JSON" });
    }

    let message;
    try {
      message = toBindingMessage(payload);
    } catch (err) {
      return json(400, { success: false, code: "E_INVALID_REQUEST", error: err.message });
    }

    try {
      const result = await env.EMAIL.send(message);
      return json(200, { success: true, messageId: result?.messageId ?? "" });
    } catch (err) {
      const code = err?.code || "E_SEND_FAILED";
      return json(statusForCode(code), {
        success: false,
        code,
        error: err?.message || "Send failed",
      });
    }
  },
};

/**
 * Compares the presented bearer token against the configured secret without
 * leaking its length or a matching prefix through timing.
 */
function authorized(request, secret) {
  const header = request.headers.get("authorization") || "";
  const presented = header.startsWith("Bearer ") ? header.slice(7) : "";
  if (presented.length !== secret.length) {
    return false;
  }
  let diff = 0;
  for (let i = 0; i < secret.length; i++) {
    diff |= presented.charCodeAt(i) ^ secret.charCodeAt(i);
  }
  return diff === 0;
}

/**
 * Translates the relay's REST-shaped payload into the shape the `send_email`
 * binding expects, validating the fields the binding requires.
 */
function toBindingMessage(payload) {
  const from = toBindingAddress(payload.from);
  if (!from) {
    throw new Error("from is required");
  }

  const to = toBindingAddressList(payload.to);
  const cc = toBindingAddressList(payload.cc);
  const bcc = toBindingAddressList(payload.bcc);

  if (to.length === 0 && cc.length === 0 && bcc.length === 0) {
    throw new Error("at least one recipient is required");
  }
  if (!payload.subject) {
    throw new Error("subject is required");
  }
  if (!payload.text && !payload.html) {
    throw new Error("either text or html is required");
  }

  const message = { from, subject: payload.subject };

  if (to.length > 0) message.to = to;
  if (cc.length > 0) message.cc = cc;
  if (bcc.length > 0) message.bcc = bcc;
  if (payload.text) message.text = payload.text;
  if (payload.html) message.html = payload.html;
  if (payload.reply_to) message.replyTo = payload.reply_to;
  if (payload.headers && Object.keys(payload.headers).length > 0) {
    message.headers = payload.headers;
  }

  return message;
}

/** Converts one REST-shaped address (`address`/`name`) into the binding's `email`/`name`. */
function toBindingAddress(value) {
  if (!value) return null;
  if (typeof value === "string") return value;
  if (!value.address) return null;
  return value.name ? { email: value.address, name: value.name } : { email: value.address };
}

function toBindingAddressList(value) {
  if (!value) return [];
  const items = Array.isArray(value) ? value : [value];
  return items.map(toBindingAddress).filter(Boolean);
}

/**
 * Maps a binding error code to the HTTP status the relay classifies on.
 * Rate limits are temporary; sender and recipient rejections are permanent.
 */
function statusForCode(code) {
  switch (code) {
    case "E_RATE_LIMIT_EXCEEDED":
    case "E_DAILY_LIMIT_EXCEEDED":
      return 429;
    case "E_SENDER_NOT_VERIFIED":
    case "E_SENDER_DOMAIN_NOT_AVAILABLE":
    case "E_RECIPIENT_NOT_ALLOWED":
      return 403;
    default:
      return 502;
  }
}

function json(status, body) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}
