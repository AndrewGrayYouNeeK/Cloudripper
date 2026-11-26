import assert from "node:assert/strict";
import test from "node:test";
import { call, makeEnv, proUser, ripReport, signIn } from "./test-helpers.js";

test("token creation needs Pro, shows the token once, and stores only a hash", async () => {
  const env = makeEnv();
  const { cookie } = await signIn(env);
  const denied = await call(env, "/tokens", { method: "POST", form: { name: "x" }, cookie });
  assert.equal(denied.status, 402);

  const pro = await proUser(env, "bob@example.com", "cus_b", "sub_b");
  assert.match(pro.token, /^crp_[\w-]{40,}$/);
  const row = await env.DB.prepare("SELECT * FROM api_tokens").first();
  assert.ok(!JSON.stringify(row).includes(pro.token));
  assert.equal(row.prefix, pro.token.slice(0, 10));
  const later = await (await call(env, "/dashboard", { cookie: pro.cookie })).text();
  assert.ok(!later.includes(pro.token), "token is not shown again");
  assert.match(later, /laptop/);
});

test("dashboard escapes user-controlled text", async () => {
  const env = makeEnv();
  const { token, cookie } = await proUser(env);
  const report = ripReport();
  report.resources[0].name = "<img src=x onerror=alert(1)>";
  report.warnings = ["<b>warn</b>"];
  const up = await call(env, "/api/v1/reports", { method: "POST", token, body: { account: "prod/aws@1", report } });
  const { report: stored } = await up.json();
  const dash = await (await call(env, "/dashboard", { cookie })).text();
  assert.match(dash, /prod\/aws@1/);
  const detail = await call(env, `/reports/${stored.id}`, { cookie });
  const body = await detail.text();
  assert.ok(!body.includes("<img src=x"));
  assert.ok(body.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.ok(body.includes("&lt;b&gt;warn&lt;/b&gt;"));
  assert.match(detail.headers.get("content-security-policy"), /default-src 'none'/);
});

test("dashboard shows subscribe buttons for each configured plan", async () => {
  const env = makeEnv({ STRIPE_PRICE_ANNUAL: "" });
  const { cookie } = await signIn(env);
  const dash = await (await call(env, "/dashboard", { cookie })).text();
  assert.match(dash, /Subscribe · Monthly/);
  assert.ok(!dash.includes("Subscribe · Annual"));
});

test("email summary setting can be turned off", async () => {
  const env = makeEnv();
  const { cookie } = await signIn(env);
  await call(env, "/settings", { method: "POST", form: {}, cookie });
  assert.equal((await env.DB.prepare("SELECT email_summaries FROM users").first()).email_summaries, 0);
  await call(env, "/settings", { method: "POST", form: { email_summaries: "1" }, cookie });
  assert.equal((await env.DB.prepare("SELECT email_summaries FROM users").first()).email_summaries, 1);
});

test("unknown routes return 404", async () => {
  const env = makeEnv();
  assert.equal((await call(env, "/nope")).status, 303, "signed-out GET goes to sign-in");
  const { cookie } = await signIn(env);
  assert.equal((await call(env, "/nope", { cookie })).status, 404);
  assert.equal((await call(env, "/api/v1/nope", { cookie })).status, 404);
});
