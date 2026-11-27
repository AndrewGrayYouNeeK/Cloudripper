import assert from "node:assert/strict";
import test from "node:test";
import worker from "./index.js";
import { buildSummary, sendSummaries, SUMMARY_MIN_INTERVAL_MS } from "./summary.js";
import { call, makeEnv, proUser, ripReport, signIn } from "./test-helpers.js";

async function upload(env, token, account, report) {
  const res = await call(env, "/api/v1/reports", { method: "POST", token, body: { account, report } });
  assert.equal(res.status, 201);
}

test("summary compares each account's latest scan with the one before", async () => {
  const env = makeEnv();
  const { token, user } = await proUser(env);
  await upload(env, token, "prod", ripReport({ estimated: 100, actual: 90 }));
  await new Promise((r) => setTimeout(r, 2));
  await upload(env, token, "prod", ripReport({ estimated: 120.5, actual: 80 }));
  await upload(env, token, "dev", ripReport({ estimated: 5, actual: 4, resources: 1 }));
  const rows = await buildSummary(env, user.id);
  assert.deepEqual(
    rows.map((r) => [r.label, r.estimated_total, r.estimated_change, r.actual_change]),
    [
      ["dev", 5, null, null],
      ["prod", 120.5, 20.5, -10],
    ],
  );
});

test("sendSummaries emails active subscribers once per interval", async () => {
  const env = makeEnv();
  const { token } = await proUser(env);
  await upload(env, token, "prod", ripReport());
  const before = env.EMAIL.sent.length;
  const now = Date.now();
  assert.deepEqual(await sendSummaries(env, now), { sent: 1, skipped: 0, failed: 0 });
  const msg = env.EMAIL.sent.at(-1);
  assert.equal(env.EMAIL.sent.length, before + 1);
  assert.equal(msg.to, "ada@example.com");
  assert.equal(msg.subject, "Your CloudRipper Pro summary");
  assert.match(msg.text, /prod: 2 resources, estimated \$120.50/);
  assert.match(msg.text, /https:\/\/pro.test\/dashboard/);
  assert.ok(msg.html.includes("<table"));
  assert.deepEqual(await sendSummaries(env, now + 1000), { sent: 0, skipped: 0, failed: 0 });
  assert.equal((await sendSummaries(env, now + SUMMARY_MIN_INTERVAL_MS + 1)).sent, 1);
});

test("no summary for opted-out, non-Pro, or report-less users", async () => {
  const env = makeEnv();
  await signIn(env, "free@example.com");
  const pro = await proUser(env, "quiet@example.com", "cus_q", "sub_q");
  await upload(env, pro.token, "prod", ripReport());
  await call(env, "/settings", { method: "POST", form: {}, cookie: pro.cookie });
  await proUser(env, "empty@example.com", "cus_e", "sub_e");
  const before = env.EMAIL.sent.length;
  assert.deepEqual(await sendSummaries(env), { sent: 0, skipped: 1, failed: 0 });
  assert.equal(env.EMAIL.sent.length, before);
});

test("a failed send is counted and retried next run", async () => {
  const env = makeEnv();
  const { token } = await proUser(env);
  await upload(env, token, "prod", ripReport());
  env.EMAIL.send = async () => {
    throw Object.assign(new Error("boom"), { code: "E_TEST" });
  };
  const errors = console.error;
  console.error = () => {};
  try {
    assert.deepEqual(await sendSummaries(env), { sent: 0, skipped: 0, failed: 1 });
  } finally {
    console.error = errors;
  }
  assert.equal((await env.DB.prepare("SELECT last_summary_at FROM users").first()).last_summary_at, null);
});

test("the cron handler runs sendSummaries", async () => {
  const env = makeEnv();
  const { token } = await proUser(env);
  await upload(env, token, "prod", ripReport());
  const waits = [];
  const log = console.log;
  console.log = () => {};
  try {
    await worker.scheduled({ cron: "0 13 * * 1" }, env, { waitUntil: (p) => waits.push(p) });
    await Promise.all(waits);
  } finally {
    console.log = log;
  }
  assert.equal(env.EMAIL.sent.at(-1).subject, "Your CloudRipper Pro summary");
});
