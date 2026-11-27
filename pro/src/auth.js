// Email magic-link sign-in, browser sessions, and API-token authentication.
// TODO(stage 2): GitHub and Google OAuth sign-in. Both should resolve to the
// same users row by verified email and then call createSession().

import { sendMagicLinkEmail } from "./email.js";
import { appOrigin, newId, normalizeEmail, parseCookies, randomToken, sha256Hex } from "./util.js";

export const SESSION_COOKIE = "cr_session";
export const MAGIC_LINK_TTL_MS = 15 * 60 * 1000;
export const SESSION_TTL_MS = 30 * 24 * 60 * 60 * 1000;
export const MAGIC_LINKS_PER_HOUR = 5;
export const API_TOKEN_PREFIX = "crp_";

/**
 * Creates a one-time sign-in link and emails it. Returns { sent, reason }.
 * Callers show the same message either way so the form does not reveal
 * whether an address has an account.
 */
export async function requestMagicLink(env, rawEmail, now = Date.now()) {
  const email = normalizeEmail(rawEmail);
  if (!email) return { sent: false, reason: "invalid_email" };

  const recent = await env.DB.prepare("SELECT COUNT(*) AS n FROM magic_links WHERE email = ? AND created_at > ?")
    .bind(email, now - 60 * 60 * 1000)
    .first();
  if ((recent?.n ?? 0) >= MAGIC_LINKS_PER_HOUR) return { sent: false, reason: "rate_limited" };

  const token = randomToken(32);
  await env.DB.prepare("INSERT INTO magic_links (token_hash, email, created_at, expires_at) VALUES (?, ?, ?, ?)")
    .bind(await sha256Hex(token), email, now, now + MAGIC_LINK_TTL_MS)
    .run();

  const link = `${appOrigin(env)}/auth/verify?token=${encodeURIComponent(token)}`;
  await sendMagicLinkEmail(env, email, link);
  return { sent: true };
}

/**
 * Consumes a magic-link token. Returns the user row or null when the token is
 * unknown, used, or expired. Creates the user on first sign-in.
 */
export async function consumeMagicLink(env, token, now = Date.now()) {
  if (typeof token !== "string" || token.length < 20 || token.length > 200) return null;
  const tokenHash = await sha256Hex(token);
  const claimed = await env.DB.prepare(
    "UPDATE magic_links SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ? RETURNING email",
  )
    .bind(now, tokenHash, now)
    .first();
  if (!claimed) return null;

  await env.DB.prepare("INSERT INTO users (id, email, created_at) VALUES (?, ?, ?) ON CONFLICT (email) DO NOTHING")
    .bind(newId(), claimed.email, now)
    .run();
  return env.DB.prepare("SELECT * FROM users WHERE email = ?").bind(claimed.email).first();
}

/** Creates a session and returns the Set-Cookie header value. */
export async function createSession(env, userId, now = Date.now()) {
  const raw = randomToken(32);
  await env.DB.prepare("INSERT INTO sessions (id_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)")
    .bind(await sha256Hex(raw), userId, now, now + SESSION_TTL_MS)
    .run();
  return `${SESSION_COOKIE}=${raw}; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=${Math.floor(SESSION_TTL_MS / 1000)}`;
}

export function clearSessionCookie() {
  return `${SESSION_COOKIE}=; Path=/; HttpOnly; Secure; SameSite=Lax; Max-Age=0`;
}

/** Returns the signed-in user for a browser request, or null. */
export async function sessionUser(request, env, now = Date.now()) {
  const raw = parseCookies(request)[SESSION_COOKIE];
  if (!raw || raw.length > 200) return null;
  return env.DB.prepare(
    "SELECT users.* FROM sessions JOIN users ON users.id = sessions.user_id WHERE sessions.id_hash = ? AND sessions.expires_at > ?",
  )
    .bind(await sha256Hex(raw), now)
    .first();
}

export async function destroySession(request, env) {
  const raw = parseCookies(request)[SESSION_COOKIE];
  if (!raw || raw.length > 200) return;
  await env.DB.prepare("DELETE FROM sessions WHERE id_hash = ?").bind(await sha256Hex(raw)).run();
}

/** Returns the user for an `Authorization: Bearer crp_...` request, or null. */
export async function tokenUser(request, env, now = Date.now()) {
  const header = request.headers.get("authorization") || "";
  const match = /^Bearer\s+(\S+)$/i.exec(header);
  if (!match || !match[1].startsWith(API_TOKEN_PREFIX) || match[1].length > 200) return null;
  const row = await env.DB.prepare(
    "SELECT users.*, api_tokens.id AS token_id FROM api_tokens JOIN users ON users.id = api_tokens.user_id WHERE api_tokens.token_hash = ? AND api_tokens.revoked_at IS NULL",
  )
    .bind(await sha256Hex(match[1]))
    .first();
  if (!row) return null;
  await env.DB.prepare("UPDATE api_tokens SET last_used_at = ? WHERE id = ?").bind(now, row.token_id).run();
  return row;
}

/** Bearer token first (CLI/desktop), then the browser session cookie. */
export async function requestUser(request, env, now = Date.now()) {
  if (request.headers.has("authorization")) return tokenUser(request, env, now);
  return sessionUser(request, env, now);
}
