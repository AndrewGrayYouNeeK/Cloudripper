// API tokens used by `cloudripper upload`. The plaintext is shown once at
// creation; only its SHA-256 hash and a short display prefix are stored.

import { API_TOKEN_PREFIX } from "./auth.js";
import { newId, randomToken, sha256Hex } from "./util.js";

export const MAX_ACTIVE_TOKENS = 20;

export async function createApiToken(env, userId, rawName, now = Date.now()) {
  const name = typeof rawName === "string" ? rawName.trim().slice(0, 60) : "";
  if (!name) return { error: "Token name is required." };
  const active = await env.DB.prepare("SELECT COUNT(*) AS n FROM api_tokens WHERE user_id = ? AND revoked_at IS NULL")
    .bind(userId)
    .first();
  if ((active?.n ?? 0) >= MAX_ACTIVE_TOKENS) return { error: `You can have at most ${MAX_ACTIVE_TOKENS} active tokens.` };

  const token = API_TOKEN_PREFIX + randomToken(32);
  const id = newId();
  await env.DB.prepare("INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, created_at) VALUES (?, ?, ?, ?, ?, ?)")
    .bind(id, userId, name, await sha256Hex(token), token.slice(0, API_TOKEN_PREFIX.length + 6), now)
    .run();
  return { id, name, token, prefix: token.slice(0, API_TOKEN_PREFIX.length + 6), created_at: now };
}

export async function listApiTokens(env, userId) {
  const { results } = await env.DB.prepare(
    "SELECT id, name, prefix, created_at, last_used_at FROM api_tokens WHERE user_id = ? AND revoked_at IS NULL ORDER BY created_at DESC",
  )
    .bind(userId)
    .all();
  return results;
}

export async function revokeApiToken(env, userId, tokenId, now = Date.now()) {
  const result = await env.DB.prepare("UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL")
    .bind(now, tokenId, userId)
    .run();
  return (result?.meta?.changes ?? 0) > 0;
}
