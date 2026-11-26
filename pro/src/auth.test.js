import assert from "node:assert/strict";
import test from "node:test";
import { consumeMagicLink, MAGIC_LINK_TTL_MS, MAGIC_LINKS_PER_HOUR, requestMagicLink } from "./auth.js";
import { call, makeEnv, signIn } from "./test-helpers.js";
import { sha256Hex } from "./util.js";

function linkToken(env) {
  const link = new URL(/https:\/\/\S+/.exec(env.EMAIL.sent.at(-1).text)[0]);
  return link.searchParams.get("token");
}

test("magic link email goes to the address with a one-time link and stores only a hash", async () => {
  const env = makeEnv();
  const res = await call(env, "/auth/magic-link", { method: "POST", form: { email: " Ada@Example.com " } });
  assert.equal(res.status, 200);
  assert.match(await res.text(), /a link is on its way/);
  assert.equal(env.EMAIL.sent.length, 1);
  const msg = env.EMAIL.sent[0];
  assert.equal(msg.to, "ada@example.com");
  assert.deepEqual(msg.from, { email: "pro@mail.test", name: "CloudRipper Pro" });
  assert.ok(msg.text && msg.html);
  const token = linkToken(env);
  assert.ok(msg.text.startsWith("Sign in") && msg.text.includes("https://pro.test/auth/verify?token="));
  const row = await env.DB.prepare("SELECT * FROM magic_links").first();
  assert.equal(row.token_hash, await sha256Hex(token));
  assert.ok(!JSON.stringify(row).includes(token));
});

test("invalid email is rejected without sending", async () => {
  const env = makeEnv();
  const res = await call(env, "/auth/magic-link", { method: "POST", form: { email: "nope" } });
  assert.equal(res.status, 400);
  assert.equal(env.EMAIL.sent.length, 0);
});

test("magic links are rate limited per address", async () => {
  const env = makeEnv();
  for (let i = 0; i < MAGIC_LINKS_PER_HOUR; i++) assert.equal((await requestMagicLink(env, "a@b.co")).sent, true);
  assert.deepEqual(await requestMagicLink(env, "a@b.co"), { sent: false, reason: "rate_limited" });
  const res = await call(env, "/auth/magic-link", { method: "POST", form: { email: "a@b.co" } });
  assert.equal(res.status, 200, "rate limiting shows the same generic message");
  assert.equal(env.EMAIL.sent.length, MAGIC_LINKS_PER_HOUR);
});

test("GET verify shows a confirm button and does not use the token", async () => {
  const env = makeEnv();
  await requestMagicLink(env, "ada@example.com");
  const token = linkToken(env);
  const page = await call(env, `/auth/verify?token=${encodeURIComponent(token)}`);
  assert.equal(page.status, 200);
  const body = await page.text();
  assert.match(body, /method="post" action="\/auth\/verify"/);
  assert.equal((await env.DB.prepare("SELECT used_at FROM magic_links").first()).used_at, null);
});

test("POST verify creates the user and a secure session cookie, once", async () => {
  const env = makeEnv();
  await requestMagicLink(env, "ada@example.com");
  const token = linkToken(env);
  const res = await call(env, "/auth/verify", { method: "POST", form: { token } });
  assert.equal(res.status, 303);
  assert.equal(res.headers.get("location"), "/dashboard");
  const cookie = res.headers.get("set-cookie");
  assert.match(cookie, /^cr_session=[\w-]+; Path=\/; HttpOnly; Secure; SameSite=Lax; Max-Age=\d+$/);
  const dash = await call(env, "/dashboard", { cookie: cookie.split(";")[0] });
  assert.equal(dash.status, 200);
  assert.match(await dash.text(), /ada@example.com/);

  const again = await call(env, "/auth/verify", { method: "POST", form: { token } });
  assert.equal(again.status, 400, "a used token cannot sign in again");
});

test("expired tokens are refused", async () => {
  const env = makeEnv();
  const t0 = Date.now() - MAGIC_LINK_TTL_MS - 1000;
  await requestMagicLink(env, "ada@example.com", t0);
  assert.equal(await consumeMagicLink(env, linkToken(env)), null);
});

test("signing in twice keeps one user row", async () => {
  const env = makeEnv();
  await signIn(env, "ada@example.com");
  await signIn(env, "ada@example.com");
  assert.equal((await env.DB.prepare("SELECT COUNT(*) AS n FROM users").first()).n, 1);
});

test("POST verify from another origin is blocked (login CSRF)", async () => {
  const env = makeEnv();
  await requestMagicLink(env, "ada@example.com");
  const res = await call(env, "/auth/verify", { method: "POST", form: { token: linkToken(env) }, origin: "https://evil.test" });
  assert.equal(res.status, 403);
});

test("dashboard redirects when signed out, and logout ends the session", async () => {
  const env = makeEnv();
  assert.equal((await call(env, "/dashboard")).headers.get("location"), "/");
  const { cookie } = await signIn(env);
  const out = await call(env, "/auth/logout", { method: "POST", cookie });
  assert.equal(out.status, 303);
  assert.match(out.headers.get("set-cookie"), /Max-Age=0/);
  assert.equal((await call(env, "/dashboard", { cookie })).status, 303);
});

test("expired sessions are not accepted", async () => {
  const env = makeEnv();
  const { cookie } = await signIn(env);
  await env.DB.prepare("UPDATE sessions SET expires_at = ?").bind(Date.now() - 1).run();
  assert.equal((await call(env, "/dashboard", { cookie })).status, 303);
});

test("cookie-authenticated POSTs require our Origin", async () => {
  const env = makeEnv();
  const { cookie } = await signIn(env);
  const res = await call(env, "/settings", { method: "POST", form: { email_summaries: "0" }, cookie, origin: "https://evil.test" });
  assert.equal(res.status, 403);
  const none = await call(env, "/settings", { method: "POST", form: { email_summaries: "0" }, cookie, origin: null });
  assert.equal(none.status, 403);
});
