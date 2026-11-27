import assert from "node:assert/strict";
import test from "node:test";
import { MAX_REPORT_BYTES, summarizeReport } from "./reports.js";
import { call, makeEnv, proUser, ripReport, signedWebhook, signIn, subscriptionEvent } from "./test-helpers.js";

async function upload(env, token, body) {
  const res = await call(env, "/api/v1/reports", { method: "POST", token, body });
  return { status: res.status, json: await res.json() };
}

test("upload needs a valid API token", async () => {
  const env = makeEnv();
  assert.equal((await upload(env, null, { account: "prod", report: ripReport() })).status, 401);
  assert.equal((await upload(env, "crp_notreal", { account: "prod", report: ripReport() })).status, 401);
  const { cookie } = await signIn(env);
  const res = await call(env, "/api/v1/reports", { method: "POST", cookie, body: { account: "prod", report: ripReport() } });
  assert.equal(res.status, 401, "a browser session cannot upload");
});

test("upload stores the JSON in R2 and summary metadata in D1", async () => {
  const env = makeEnv();
  const { token, user } = await proUser(env);
  const { status, json } = await upload(env, token, { account: "prod-aws", generated_at: "2026-11-20T10:00:00Z", report: ripReport() });
  assert.equal(status, 201);
  assert.equal(json.report.kind, "rip");
  assert.equal(json.report.resource_count, 2);
  assert.equal(json.report.estimated_total, 120.5);
  assert.equal(json.report.actual_total, 98.25);
  assert.equal(json.report.warning_count, 1);
  assert.equal(json.report.providers, "aws,gcp");
  const row = await env.DB.prepare("SELECT * FROM scan_reports").first();
  assert.equal(row.user_id, user.id);
  assert.equal(row.generated_at, Date.parse("2026-11-20T10:00:00Z"));
  assert.equal(row.r2_key, `reports/${user.id}/${row.account_id}/${row.id}.json`);
  assert.deepEqual(JSON.parse(env.REPORTS.objects.get(row.r2_key)), ripReport());
  const token_row = await env.DB.prepare("SELECT last_used_at FROM api_tokens").first();
  assert.ok(token_row.last_used_at > 0);
});

test("upload needs an active subscription", async () => {
  const env = makeEnv();
  const { token, user } = await proUser(env);
  await signedWebhook(env, subscriptionEvent(user.id, { type: "customer.subscription.updated", status: "past_due" }));
  const res = await upload(env, token, { account: "prod", report: ripReport() });
  assert.equal(res.status, 402);
});

test("multiple accounts, history, filters, and report detail", async () => {
  const env = makeEnv();
  const { token, cookie } = await proUser(env);
  await upload(env, token, { account: "prod-aws", report: ripReport({ estimated: 100 }) });
  await upload(env, token, { account: "prod-aws", kind: "cost", report: ripReport({ estimated: 110 }) });
  await upload(env, token, { account: "staging gcp", report: ripReport({ estimated: 5 }) });
  await upload(env, token, {
    account: "prod-aws",
    report: { TotalMonthlyCost: 110, PotentialSavings: 30.5, Recommendations: [{ Resource: { Provider: "aws", ID: "i-1" }, Action: "rightsize", Savings: 30.5 }] },
  });

  const accounts = await (await call(env, "/api/v1/accounts", { token })).json();
  assert.deepEqual(accounts.accounts.map((a) => [a.label, a.report_count]), [["prod-aws", 3], ["staging gcp", 1]]);

  const prod = accounts.accounts[0];
  const history = await (await call(env, `/api/v1/reports?account=${prod.id}`, { token })).json();
  assert.equal(history.reports.length, 3);
  assert.deepEqual(new Set(history.reports.map((r) => r.kind)), new Set(["rip", "cost", "optimize"]));
  const opt = history.reports.find((r) => r.kind === "optimize");
  assert.equal(opt.potential_savings, 30.5);
  assert.equal(opt.recommendation_count, 1);
  assert.equal(opt.estimated_total, 110);

  const onlyCost = await (await call(env, "/api/v1/reports?kind=cost", { token })).json();
  assert.equal(onlyCost.reports.length, 1);
  assert.equal((await call(env, "/api/v1/reports?kind=bogus", { token })).status, 400);
  const limited = await (await call(env, "/api/v1/reports?limit=2", { cookie })).json();
  assert.equal(limited.reports.length, 2, "session cookie can read too");

  const detail = await (await call(env, `/api/v1/reports/${history.reports[0].id}`, { token })).json();
  assert.ok(detail.report);
  assert.equal(detail.r2_key, undefined, "storage keys are not exposed");

  const page = await call(env, `/reports/${opt.id}`, { cookie });
  assert.equal(page.status, 200);
  assert.match(await page.text(), /rightsize/);
  const dash = await (await call(env, `/dashboard?account=${prod.id}`, { cookie })).text();
  assert.match(dash, /History · prod-aws/);
});

test("users cannot read each other's reports", async () => {
  const env = makeEnv();
  const ada = await proUser(env, "ada@example.com", "cus_a", "sub_a");
  const bob = await proUser(env, "bob@example.com", "cus_b", "sub_b");
  const { json } = await upload(env, ada.token, { account: "prod", report: ripReport() });
  assert.equal((await call(env, `/api/v1/reports/${json.report.id}`, { token: bob.token })).status, 404);
  assert.equal((await call(env, `/reports/${json.report.id}`, { cookie: bob.cookie })).status, 404);
  const bobAccounts = await (await call(env, "/api/v1/accounts", { token: bob.token })).json();
  assert.deepEqual(bobAccounts.accounts, []);
  const filtered = await (await call(env, `/api/v1/reports?account=${json.report.account.id}`, { token: bob.token })).json();
  assert.deepEqual(filtered.reports, []);
});

test("bad uploads are rejected with a reason", async () => {
  const env = makeEnv();
  const { token } = await proUser(env);
  const cases = [
    ["not json", 400],
    [{ report: ripReport() }, 400],
    [{ account: "<script>", report: ripReport() }, 400],
    [{ account: "prod", report: [] }, 400],
    [{ account: "prod", report: { hello: 1 } }, 400],
    [{ account: "prod", kind: "chaos", report: ripReport() }, 400],
    [{ account: "prod", kind: "optimize", report: ripReport() }, 400],
    [{ account: "prod", generated_at: "yesterday", report: ripReport() }, 400],
    [JSON.stringify({ account: "prod", report: { resources: [], pad: "x".repeat(MAX_REPORT_BYTES) } }), 413],
  ];
  for (const [body, status] of cases) {
    const res = await upload(env, token, body);
    assert.equal(res.status, status, JSON.stringify(body).slice(0, 80));
    assert.equal(res.json.ok, false);
  }
  assert.equal(env.REPORTS.objects.size, 0);
});

test("revoked tokens stop working", async () => {
  const env = makeEnv();
  const { token, cookie } = await proUser(env);
  const { id } = await env.DB.prepare("SELECT id FROM api_tokens").first();
  const res = await call(env, `/tokens/${id}/revoke`, { method: "POST", cookie });
  assert.equal(res.status, 303);
  assert.equal((await upload(env, token, { account: "prod", report: ripReport() })).status, 401);
});

test("summaries handle unpriced actuals and optimize --apply output", () => {
  const scan = summarizeReport("rip", { estimated_total: 3, actual_total: 0, actual_available: false, resources: [] });
  assert.equal(scan.actual_total, null);
  assert.equal(scan.actual_available, 0);
  const applied = summarizeReport("optimize", { monthly_cost: 50, potential_savings: 12, recommendations: [], records: [] });
  assert.equal(applied.estimated_total, 50);
  assert.equal(applied.potential_savings, 12);
});
