// Shared helpers: responses, cookies, hashing, ids, and HTML escaping.

const encoder = new TextEncoder();

export function json(data, status = 200, headers = {}) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { "content-type": "application/json; charset=utf-8", "cache-control": "no-store", ...headers },
  });
}

export function html(body, status = 200, headers = {}) {
  return new Response(body, {
    status,
    headers: {
      "content-type": "text/html; charset=utf-8",
      "cache-control": "no-store",
      "x-frame-options": "DENY",
      "referrer-policy": "no-referrer",
      "content-security-policy": "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' https://checkout.stripe.com https://billing.stripe.com; base-uri 'none'; frame-ancestors 'none'",
      ...headers,
    },
  });
}

export function redirect(location, status = 303, headers = {}) {
  return new Response(null, { status, headers: { location, "cache-control": "no-store", ...headers } });
}

export function errorJson(status, message) {
  return json({ ok: false, error: message }, status);
}

export function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

export function toHex(buffer) {
  return [...new Uint8Array(buffer)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

export async function sha256Hex(value) {
  return toHex(await crypto.subtle.digest("SHA-256", encoder.encode(value)));
}

export async function hmacSha256Hex(secret, message) {
  const key = await crypto.subtle.importKey("raw", encoder.encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return toHex(await crypto.subtle.sign("HMAC", key, encoder.encode(message)));
}

/** Constant-time comparison of two strings of equal length. */
export function safeEqual(a, b) {
  if (typeof a !== "string" || typeof b !== "string" || a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return diff === 0;
}

/** URL-safe random string with `bytes` bytes of entropy. */
export function randomToken(bytes = 32) {
  const raw = crypto.getRandomValues(new Uint8Array(bytes));
  let s = "";
  for (const b of raw) s += String.fromCharCode(b);
  return btoa(s).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

export function newId() {
  return crypto.randomUUID();
}

export function parseCookies(request) {
  const out = {};
  const header = request.headers.get("cookie") || "";
  for (const part of header.split(";")) {
    const i = part.indexOf("=");
    if (i < 0) continue;
    const name = part.slice(0, i).trim();
    if (name) out[name] = decodeURIComponent(part.slice(i + 1).trim());
  }
  return out;
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function normalizeEmail(value) {
  if (typeof value !== "string") return null;
  const email = value.trim().toLowerCase();
  if (email.length > 254 || !EMAIL_RE.test(email)) return null;
  return email;
}

/** The configured public origin of the Pro app, e.g. https://pro.example.com. */
export function appOrigin(env) {
  if (!env.APP_URL) throw new Error("APP_URL is not configured");
  return new URL(env.APP_URL).origin;
}

/**
 * Cookie-authenticated state-changing requests must come from our own origin.
 * Browsers send Origin on cross-site POSTs; we reject a missing or foreign one.
 */
export function sameOrigin(request, env) {
  const origin = request.headers.get("origin");
  return origin !== null && origin === appOrigin(env);
}

export async function readForm(request) {
  const type = request.headers.get("content-type") || "";
  if (type.includes("application/json")) {
    try {
      const body = await request.json();
      return body && typeof body === "object" ? body : {};
    } catch {
      return {};
    }
  }
  if (type.includes("application/x-www-form-urlencoded") || type.includes("multipart/form-data")) {
    const form = await request.formData();
    return Object.fromEntries(form.entries());
  }
  return {};
}

export function wantsJson(request) {
  return (request.headers.get("accept") || "").includes("application/json");
}

export function formatUsd(value) {
  if (value === null || value === undefined || Number.isNaN(Number(value))) return "n/a";
  return `$${Number(value).toFixed(2)}`;
}

export function formatTime(ms) {
  if (!ms) return "never";
  return new Date(ms).toISOString().replace("T", " ").replace(/\.\d+Z$/, " UTC");
}
