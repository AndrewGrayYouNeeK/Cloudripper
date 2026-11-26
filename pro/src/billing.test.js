import assert from "node:assert/strict";
import test from "node:test";
import { activeSubscription, availablePlans, verifyStripeEvent } from "./billing.js";
import { call, makeEnv, signedWebhook, signIn, subscriptionEvent } from "./test-helpers.js";
import { hmacSha256Hex } from "./util.js";

test("webhook signatures are verified over the raw body", async () => {
  const env = makeEnv();
  const raw = JSON.stringify({ id: "evt_1", type: "ping" });
  const now = 1_800_000_000;
  const sig = await hmacSha256Hex(env.STRIPE_WEBHOOK_SECRET, `${now}.${raw}`);
  assert.deepEqual(await verifyStripeEvent(env, raw, `t=${now},v1=${sig}`, now), { id: "evt_1", type: "ping" });
  assert.equal(await verifyStripeEvent(env, raw, `t=${now},v1=${"0".repeat(64)}`, now), null);
  assert.equal(await verifyStripeEvent(env, raw + " ", `t=${now},v1=${sig}`, now), null);
  assert.equal(await verifyStripeEvent(env, raw, `t=${now},v1=${sig}`, now + 301), null, "stale");
  assert.equal(await verifyStripeEvent(env, raw, null, now), null);
  assert.equal(await verifyStripeEvent({ ...env, STRIPE_WEBHOOK_SECRET: "" }, raw, `t=${now},v1=${sig}`, now), null);
});

test("webhook endpoint rejects a bad signature", async () => {
  const env = makeEnv();
  const res = await signedWebhook(env, { id: "evt_x", type: "customer.subscription.created" }, { secret: "whsec_wrong" });
  assert.equal(res.status, 400);
});

test("subscription events grant and revoke Pro, monthly and annual", async () => {
  const env = makeEnv();
  const { user } = await signIn(env);
  assert.equal(await activeSubscription(env, user.id), null);

  await signedWebhook(env, subscriptionEvent(user.id, { price: "price_annual_test" }));
  let sub = await activeSubscription(env, user.id);
  assert.equal(sub.plan, "annual");
  assert.equal(sub.current_period_end, 1_900_000_000 * 1000);
  assert.equal((await env.DB.prepare("SELECT stripe_customer_id FROM users").first()).stripe_customer_id, "cus_1");

  await signedWebhook(env, subscriptionEvent(user.id, { type: "customer.subscription.updated", price: "price_monthly_test" }));
  assert.equal((await activeSubscription(env, user.id)).plan, "monthly");

  await signedWebhook(env, subscriptionEvent(user.id, { type: "customer.subscription.deleted", status: "canceled" }));
  assert.equal(await activeSubscription(env, user.id), null);
});

test("duplicate and out-of-order events do not regress status", async () => {
  const env = makeEnv();
  const { user } = await signIn(env);
  const newer = subscriptionEvent(user.id, { status: "active", created: 2_000 });
  const older = subscriptionEvent(user.id, { type: "customer.subscription.updated", status: "incomplete", created: 1_000 });
  await signedWebhook(env, newer);
  const dup = await (await signedWebhook(env, newer)).json();
  assert.equal(dup.result.duplicate, true);
  await signedWebhook(env, older);
  assert.equal((await activeSubscription(env, user.id)).status, "active");
});

test("subscription found by customer id when metadata is missing", async () => {
  const env = makeEnv();
  const { user } = await signIn(env);
  await signedWebhook(env, { id: "evt_c", type: "checkout.session.completed", data: { object: { client_reference_id: user.id, customer: "cus_9" } } });
  const event = subscriptionEvent(user.id, { customer: "cus_9" });
  delete event.data.object.metadata;
  await signedWebhook(env, event);
  assert.equal((await activeSubscription(env, user.id)).status, "active");
});

test("events for unknown users are acknowledged and ignored", async () => {
  const env = makeEnv();
  const res = await signedWebhook(env, subscriptionEvent("no-such-user", { customer: "cus_zzz" }));
  assert.equal(res.status, 200);
  assert.equal((await res.json()).result.ignored, "unknown user");
});

test("annual plan is hidden until STRIPE_PRICE_ANNUAL is configured", () => {
  assert.deepEqual(availablePlans(makeEnv()).map((p) => p.id), ["monthly", "annual"]);
  assert.deepEqual(availablePlans(makeEnv({ STRIPE_PRICE_ANNUAL: "" })).map((p) => p.id), ["monthly"]);
});

function mockFetch(responseBody) {
  const calls = [];
  const original = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    calls.push({ url: String(url), init, params: new URLSearchParams(init.body) });
    return new Response(JSON.stringify(responseBody), { status: 200 });
  };
  return { calls, restore: () => (globalThis.fetch = original) };
}

test("checkout creates a Stripe subscription session for the chosen price", async () => {
  const env = makeEnv();
  const { cookie, user } = await signIn(env);
  const stripe = mockFetch({ url: "https://checkout.stripe.com/c/pay/cs_test_1" });
  try {
    const res = await call(env, "/billing/checkout", { method: "POST", form: { plan: "annual" }, cookie });
    assert.equal(res.status, 303);
    assert.equal(res.headers.get("location"), "https://checkout.stripe.com/c/pay/cs_test_1");
    const { url, init, params } = stripe.calls[0];
    assert.equal(url, "https://api.stripe.com/v1/checkout/sessions");
    assert.equal(init.headers.authorization, "Bearer sk_test_placeholder");
    assert.equal(params.get("mode"), "subscription");
    assert.equal(params.get("line_items[0][price]"), "price_annual_test");
    assert.equal(params.get("client_reference_id"), user.id);
    assert.equal(params.get("subscription_data[metadata][user_id]"), user.id);
    assert.equal(params.get("customer_email"), "ada@example.com");
    assert.equal(params.get("success_url"), "https://pro.test/dashboard?checkout=success");

    const bad = await call(env, "/billing/checkout", { method: "POST", form: { plan: "lifetime" }, cookie });
    assert.equal(bad.status, 400);
  } finally {
    stripe.restore();
  }
});

test("portal needs a Stripe customer and returns to the dashboard", async () => {
  const env = makeEnv();
  const { cookie, user } = await signIn(env);
  assert.equal((await call(env, "/billing/portal", { method: "POST", cookie })).status, 400);
  await signedWebhook(env, subscriptionEvent(user.id, { customer: "cus_7" }));
  const stripe = mockFetch({ url: "https://billing.stripe.com/p/session/test_1" });
  try {
    const res = await call(env, "/billing/portal", { method: "POST", cookie });
    assert.equal(res.headers.get("location"), "https://billing.stripe.com/p/session/test_1");
    assert.equal(stripe.calls[0].url, "https://api.stripe.com/v1/billing_portal/sessions");
    assert.equal(stripe.calls[0].params.get("customer"), "cus_7");
    assert.equal(stripe.calls[0].params.get("return_url"), "https://pro.test/dashboard");
  } finally {
    stripe.restore();
  }
});

test("/api/v1/me reports Pro status", async () => {
  const env = makeEnv();
  const { cookie, user } = await signIn(env);
  assert.equal((await (await call(env, "/api/v1/me", { cookie })).json()).pro, false);
  await signedWebhook(env, subscriptionEvent(user.id));
  const me = await (await call(env, "/api/v1/me", { cookie })).json();
  assert.equal(me.pro, true);
  assert.equal(me.plan, "monthly");
});
