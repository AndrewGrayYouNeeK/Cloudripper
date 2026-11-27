// Stripe Checkout, Customer Portal, webhook verification, and Pro gating.
// No card data touches this Worker. Prices come from config:
//   STRIPE_PRICE_MONTHLY (the $29/month price) and STRIPE_PRICE_ANNUAL (optional;
//   the annual option is hidden until it is set).

import { appOrigin, hmacSha256Hex, safeEqual } from "./util.js";

const STRIPE_API = "https://api.stripe.com/v1";
export const ACTIVE_STATUSES = ["active", "trialing"];
export const WEBHOOK_TOLERANCE_SECONDS = 300;

export function availablePlans(env) {
  const plans = [];
  if (env.STRIPE_PRICE_MONTHLY) plans.push({ id: "monthly", price: env.STRIPE_PRICE_MONTHLY, label: "Monthly" });
  if (env.STRIPE_PRICE_ANNUAL) plans.push({ id: "annual", price: env.STRIPE_PRICE_ANNUAL, label: "Annual" });
  return plans;
}

export function planForPrice(env, priceId) {
  if (priceId && priceId === env.STRIPE_PRICE_MONTHLY) return "monthly";
  if (priceId && priceId === env.STRIPE_PRICE_ANNUAL) return "annual";
  return null;
}

/** The user's current Pro subscription row, or null when they are not Pro. */
export async function activeSubscription(env, userId) {
  return env.DB.prepare(
    `SELECT * FROM subscriptions WHERE user_id = ? AND status IN (${ACTIVE_STATUSES.map(() => "?").join(", ")})
     ORDER BY current_period_end DESC LIMIT 1`,
  )
    .bind(userId, ...ACTIVE_STATUSES)
    .first();
}

export async function latestSubscription(env, userId) {
  return env.DB.prepare("SELECT * FROM subscriptions WHERE user_id = ? ORDER BY updated_at DESC LIMIT 1").bind(userId).first();
}

async function stripeRequest(env, path, params, fetchImpl = fetch) {
  if (!env.STRIPE_SECRET_KEY) throw new Error("STRIPE_SECRET_KEY is not configured");
  const response = await fetchImpl(`${STRIPE_API}${path}`, {
    method: "POST",
    headers: {
      authorization: `Bearer ${env.STRIPE_SECRET_KEY}`,
      "content-type": "application/x-www-form-urlencoded",
    },
    body: new URLSearchParams(params).toString(),
  });
  const data = await response.json();
  if (!response.ok) throw new Error(`Stripe ${path} failed: ${data?.error?.message ?? response.status}`);
  return data;
}

/** Creates a Checkout Session for a subscription and returns its URL. */
export async function createCheckoutSession(env, user, planId, fetchImpl = fetch) {
  const plan = availablePlans(env).find((p) => p.id === planId);
  if (!plan) return { error: "That plan is not available." };
  const origin = appOrigin(env);
  const params = {
    mode: "subscription",
    "line_items[0][price]": plan.price,
    "line_items[0][quantity]": "1",
    client_reference_id: user.id,
    "metadata[user_id]": user.id,
    "subscription_data[metadata][user_id]": user.id,
    success_url: `${origin}/dashboard?checkout=success`,
    cancel_url: `${origin}/dashboard?checkout=cancelled`,
  };
  if (user.stripe_customer_id) params.customer = user.stripe_customer_id;
  else params.customer_email = user.email;
  const session = await stripeRequest(env, "/checkout/sessions", params, fetchImpl);
  return { url: session.url };
}

/** Creates a Customer Portal session (update card, switch plan, cancel). */
export async function createPortalSession(env, user, fetchImpl = fetch) {
  if (!user.stripe_customer_id) return { error: "No billing account yet. Subscribe first." };
  const session = await stripeRequest(
    env,
    "/billing_portal/sessions",
    { customer: user.stripe_customer_id, return_url: `${appOrigin(env)}/dashboard` },
    fetchImpl,
  );
  return { url: session.url };
}

/**
 * Verifies a Stripe-Signature header (t=...,v1=...) over the raw body.
 * Returns the parsed event, or null when the signature is missing, wrong, or stale.
 */
export async function verifyStripeEvent(env, rawBody, signatureHeader, nowSeconds = Math.floor(Date.now() / 1000)) {
  if (!env.STRIPE_WEBHOOK_SECRET || !signatureHeader) return null;
  let timestamp = null;
  const signatures = [];
  for (const part of signatureHeader.split(",")) {
    const [k, v] = part.split("=", 2).map((s) => s?.trim());
    if (k === "t") timestamp = Number.parseInt(v, 10);
    if (k === "v1" && v) signatures.push(v);
  }
  if (!Number.isFinite(timestamp) || signatures.length === 0) return null;
  if (Math.abs(nowSeconds - timestamp) > WEBHOOK_TOLERANCE_SECONDS) return null;
  const expected = await hmacSha256Hex(env.STRIPE_WEBHOOK_SECRET, `${timestamp}.${rawBody}`);
  if (!signatures.some((sig) => safeEqual(sig, expected))) return null;
  try {
    return JSON.parse(rawBody);
  } catch {
    return null;
  }
}

function periodEndMs(sub) {
  // Newer Stripe API versions moved current_period_end onto subscription items.
  const seconds = sub.current_period_end ?? sub.items?.data?.[0]?.current_period_end ?? null;
  return typeof seconds === "number" ? seconds * 1000 : null;
}

async function userForSubscription(env, sub) {
  const metaUser = sub.metadata?.user_id;
  if (metaUser) {
    const row = await env.DB.prepare("SELECT * FROM users WHERE id = ?").bind(metaUser).first();
    if (row) return row;
  }
  const customer = typeof sub.customer === "string" ? sub.customer : sub.customer?.id;
  if (!customer) return null;
  return env.DB.prepare("SELECT * FROM users WHERE stripe_customer_id = ?").bind(customer).first();
}

async function upsertSubscription(env, sub, eventCreated, now) {
  const user = await userForSubscription(env, sub);
  if (!user) return { ignored: "unknown user" };
  const customer = typeof sub.customer === "string" ? sub.customer : sub.customer?.id;
  const priceId = sub.items?.data?.[0]?.price?.id ?? null;
  await env.DB.batch([
    env.DB.prepare("UPDATE users SET stripe_customer_id = ? WHERE id = ? AND stripe_customer_id IS NULL").bind(customer, user.id),
    env.DB.prepare(
      `INSERT INTO subscriptions (stripe_subscription_id, user_id, stripe_customer_id, status, plan, price_id, current_period_end, cancel_at_period_end, event_created, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT (stripe_subscription_id) DO UPDATE SET status = excluded.status, plan = excluded.plan, price_id = excluded.price_id,
         current_period_end = excluded.current_period_end, cancel_at_period_end = excluded.cancel_at_period_end,
         event_created = excluded.event_created, updated_at = excluded.updated_at
       WHERE subscriptions.event_created IS NULL OR excluded.event_created IS NULL OR excluded.event_created >= subscriptions.event_created`,
    ).bind(
      sub.id,
      user.id,
      customer,
      sub.status,
      planForPrice(env, priceId),
      priceId,
      periodEndMs(sub),
      sub.cancel_at_period_end ? 1 : 0,
      typeof eventCreated === "number" ? eventCreated : null,
      now,
    ),
  ]);
  return { user_id: user.id, status: sub.status };
}

/** Applies a verified Stripe event once. Returns a short result for logs/tests. */
export async function handleStripeEvent(env, event, now = Date.now()) {
  const seen = await env.DB.prepare("SELECT id FROM stripe_events WHERE id = ?").bind(event.id).first();
  if (seen) return { duplicate: true };
  const result = await applyStripeEvent(env, event, now);
  // Recorded only after the event is applied, so a failed attempt is retried by Stripe.
  await env.DB.prepare("INSERT INTO stripe_events (id, type, received_at) VALUES (?, ?, ?) ON CONFLICT (id) DO NOTHING")
    .bind(event.id, event.type, now)
    .run();
  return result;
}

async function applyStripeEvent(env, event, now) {
  const object = event.data?.object ?? {};
  switch (event.type) {
    case "checkout.session.completed": {
      const userId = object.client_reference_id ?? object.metadata?.user_id;
      const customer = typeof object.customer === "string" ? object.customer : object.customer?.id;
      if (!userId || !customer) return { ignored: "missing user or customer" };
      await env.DB.prepare("UPDATE users SET stripe_customer_id = ? WHERE id = ? AND stripe_customer_id IS NULL").bind(customer, userId).run();
      // Status comes from the customer.subscription.* events that Stripe sends alongside this one.
      return { linked: userId };
    }
    case "customer.subscription.created":
    case "customer.subscription.updated":
    case "customer.subscription.deleted":
      return upsertSubscription(env, object, event.created, now);
    default:
      return { ignored: event.type };
  }
}
