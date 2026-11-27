// Test doubles for the Worker bindings. D1 is backed by node:sqlite and the
// real migrations, so tests exercise the same SQL that runs in production.

import { readdirSync, readFileSync } from "node:fs";
import { DatabaseSync } from "node:sqlite";
import { hmacSha256Hex } from "./util.js";
import worker from "./index.js";

export const ORIGIN = "https://pro.test";

class Statement {
  constructor(db, sql, args = []) {
    this.db = db;
    this.sql = sql;
    this.args = args;
  }
  bind(...args) {
    for (const a of args) if (a === undefined) throw new Error(`undefined bind value in: ${this.sql}`);
    return new Statement(this.db, this.sql, args);
  }
  async first(column) {
    const row = this.db.prepare(this.sql).get(...this.args);
    if (!row) return null;
    return column ? row[column] : { ...row };
  }
  async all() {
    return { success: true, results: this.db.prepare(this.sql).all(...this.args).map((r) => ({ ...r })), meta: {} };
  }
  async run() {
    const info = this.db.prepare(this.sql).run(...this.args);
    return { success: true, meta: { changes: Number(info.changes), last_row_id: Number(info.lastInsertRowid) } };
  }
}

export function d1() {
  const db = new DatabaseSync(":memory:");
  db.exec("PRAGMA foreign_keys = ON");
  const dir = new URL("../migrations/", import.meta.url);
  for (const file of readdirSync(dir).filter((f) => f.endsWith(".sql")).sort()) {
    db.exec(readFileSync(new URL(file, dir), "utf8"));
  }
  return {
    raw: db,
    prepare: (sql) => new Statement(db, sql),
    async batch(statements) {
      db.exec("BEGIN");
      try {
        const out = [];
        for (const s of statements) out.push(await s.run());
        db.exec("COMMIT");
        return out;
      } catch (err) {
        db.exec("ROLLBACK");
        throw err;
      }
    },
  };
}

export function r2() {
  const objects = new Map();
  return {
    objects,
    async put(key, value) {
      objects.set(key, typeof value === "string" ? value : JSON.stringify(value));
    },
    async get(key) {
      if (!objects.has(key)) return null;
      const body = objects.get(key);
      return { async text() { return body; }, async json() { return JSON.parse(body); } };
    },
  };
}

export function emailBinding() {
  const sent = [];
  return {
    sent,
    async send(message) {
      sent.push(message);
      return { messageId: `msg-${sent.length}` };
    },
  };
}

export function makeEnv(overrides = {}) {
  return {
    DB: d1(),
    REPORTS: r2(),
    EMAIL: emailBinding(),
    APP_URL: ORIGIN,
    EMAIL_FROM: "pro@mail.test",
    STRIPE_SECRET_KEY: "sk_test_placeholder",
    STRIPE_WEBHOOK_SECRET: "whsec_test_placeholder",
    STRIPE_PRICE_MONTHLY: "price_monthly_test",
    STRIPE_PRICE_ANNUAL: "price_annual_test",
    ...overrides,
  };
}

export async function call(env, path, { method = "GET", body, form, headers = {}, cookie, token, origin = ORIGIN } = {}) {
  const h = new Headers(headers);
  let payload;
  if (form) {
    h.set("content-type", "application/x-www-form-urlencoded");
    payload = new URLSearchParams(form).toString();
  } else if (body !== undefined) {
    h.set("content-type", "application/json");
    payload = typeof body === "string" ? body : JSON.stringify(body);
  }
  if (method !== "GET" && origin) h.set("origin", origin);
  if (cookie) h.set("cookie", cookie);
  if (token) h.set("authorization", `Bearer ${token}`);
  return worker.fetch(new Request(`${ORIGIN}${path}`, { method, headers: h, body: payload }), env);
}

/** Signs in through the real magic-link flow and returns { cookie, user }. */
export async function signIn(env, email = "ada@example.com") {
  await call(env, "/auth/magic-link", { method: "POST", form: { email } });
  const message = env.EMAIL.sent.at(-1);
  const link = new URL(/https:\/\/\S+/.exec(message.text)[0]);
  const response = await call(env, "/auth/verify", { method: "POST", form: { token: link.searchParams.get("token") } });
  const cookie = response.headers.get("set-cookie").split(";")[0];
  const user = await env.DB.prepare("SELECT * FROM users WHERE email = ?").bind(email).first();
  return { cookie, user };
}

let eventSeq = 0;

export async function signedWebhook(env, event, { secret = env.STRIPE_WEBHOOK_SECRET, timestamp = Math.floor(Date.now() / 1000) } = {}) {
  const raw = JSON.stringify(event);
  const sig = await hmacSha256Hex(secret, `${timestamp}.${raw}`);
  return worker.fetch(
    new Request(`${ORIGIN}/stripe/webhook`, {
      method: "POST",
      headers: { "content-type": "application/json", "stripe-signature": `t=${timestamp},v1=${sig}` },
      body: raw,
    }),
    env,
  );
}

export function subscriptionEvent(userId, { type = "customer.subscription.created", status = "active", price = "price_monthly_test", id = "sub_1", customer = "cus_1", created } = {}) {
  eventSeq++;
  return {
    id: `evt_${eventSeq}_${Math.random().toString(36).slice(2)}`,
    type,
    created: created ?? 1_800_000_000 + eventSeq,
    data: {
      object: {
        id,
        object: "subscription",
        customer,
        status,
        cancel_at_period_end: false,
        metadata: { user_id: userId },
        items: { data: [{ price: { id: price }, current_period_end: 1_900_000_000 }] },
      },
    },
  };
}

/** Signs in, activates a subscription, and creates an API token. */
export async function proUser(env, email = "ada@example.com", customer = "cus_1", subId = "sub_1") {
  const { cookie, user } = await signIn(env, email);
  const res = await signedWebhook(env, subscriptionEvent(user.id, { customer, id: subId }));
  if (res.status !== 200) throw new Error(`webhook failed: ${res.status}`);
  const page = await call(env, "/tokens", { method: "POST", form: { name: "laptop" }, cookie });
  const token = /<pre>(crp_[^<]+)<\/pre>/.exec(await page.text())[1];
  return { cookie, user, token };
}

export function ripReport({ estimated = 120.5, actual = 98.25, resources = 2 } = {}) {
  return {
    dry_run: false,
    estimated_total: estimated,
    actual_total: actual,
    actual_available: true,
    warnings: ["example warning"],
    resources: Array.from({ length: resources }, (_, i) => ({
      provider: i % 2 ? "gcp" : "aws",
      type: "compute",
      id: `i-${i}`,
      name: `vm-${i}`,
      region: "us-east-1",
      status: "running",
      cost_usd: 10,
    })),
  };
}
