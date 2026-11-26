// CloudRipper Pro Worker: magic-link sign-in, report uploads and history,
// API tokens, Stripe subscription gating, and email summaries.

import {
  clearSessionCookie,
  consumeMagicLink,
  createSession,
  destroySession,
  requestMagicLink,
  requestUser,
  sessionUser,
  tokenUser,
} from "./auth.js";
import {
  activeSubscription,
  availablePlans,
  createCheckoutSession,
  createPortalSession,
  handleStripeEvent,
  latestSubscription,
  verifyStripeEvent,
} from "./billing.js";
import { dashboardPage, reportPage, signInPage, verifyPage } from "./pages.js";
import { getReport, KINDS, listAccounts, listReports, parseUpload, storeReport, UploadError } from "./reports.js";
import { sendSummaries } from "./summary.js";
import { createApiToken, listApiTokens, revokeApiToken } from "./tokens.js";
import { appOrigin, errorJson, html, json, readForm, redirect, sameOrigin } from "./util.js";

const GENERIC_LINK_MESSAGE = "If that address can sign in, a link is on its way. Check your email.";

async function renderDashboard(env, user, url, extra = {}) {
  const accounts = await listAccounts(env, user.id);
  const accountId = url.searchParams.get("account");
  const selectedAccount = accountId ? accounts.find((a) => a.id === accountId) ?? null : null;
  const [sub, lastSub, reports, tokens] = await Promise.all([
    activeSubscription(env, user.id),
    latestSubscription(env, user.id),
    listReports(env, user.id, { accountId: selectedAccount?.id ?? null, limit: 50 }),
    listApiTokens(env, user.id),
  ]);
  let flash = extra.flash ?? "";
  const checkout = url.searchParams.get("checkout");
  if (!flash && checkout === "success") flash = "Thanks. Your subscription will show here as soon as Stripe confirms it.";
  if (!flash && checkout === "cancelled") flash = "Checkout was cancelled.";
  return html(
    dashboardPage({
      user,
      sub,
      lastSub,
      plans: availablePlans(env),
      accounts,
      reports,
      tokens,
      selectedAccount,
      newToken: extra.newToken ?? null,
      flash,
      origin: appOrigin(env),
    }),
    extra.status ?? 200,
  );
}

async function handlePage(request, env, url) {
  const { pathname } = url;
  const method = request.method;

  if (pathname === "/" && method === "GET") {
    if (await sessionUser(request, env)) return redirect("/dashboard");
    return html(signInPage());
  }

  if (pathname === "/auth/magic-link" && method === "POST") {
    const form = await readForm(request);
    try {
      const result = await requestMagicLink(env, form.email);
      if (result.reason === "invalid_email") return html(signInPage({ error: "Enter a valid email address." }), 400);
    } catch (err) {
      console.error("magic link failed", err?.message);
      return html(signInPage({ error: "We could not send the email. Try again in a minute." }), 502);
    }
    return html(signInPage({ message: GENERIC_LINK_MESSAGE }));
  }

  if (pathname === "/auth/verify" && method === "GET") {
    // Showing a button instead of signing in on GET keeps mail scanners that
    // prefetch links from using up the one-time token.
    const token = url.searchParams.get("token") || "";
    if (!token) return html(signInPage({ error: "That sign-in link is incomplete." }), 400);
    return html(verifyPage(token));
  }

  if (pathname === "/auth/verify" && method === "POST") {
    if (!sameOrigin(request, env)) return html(signInPage({ error: "Request blocked." }), 403);
    const form = await readForm(request);
    const user = await consumeMagicLink(env, form.token);
    if (!user) return html(signInPage({ error: "That link has expired or was already used. Request a new one." }), 400);
    return redirect("/dashboard", 303, { "set-cookie": await createSession(env, user.id) });
  }

  if (pathname === "/stripe/webhook" && method === "POST") {
    const raw = await request.text();
    const event = await verifyStripeEvent(env, raw, request.headers.get("stripe-signature"));
    if (!event) return errorJson(400, "invalid signature");
    const result = await handleStripeEvent(env, event);
    return json({ received: true, result });
  }

  // Everything below needs a signed-in browser session.
  const user = await sessionUser(request, env);
  const isPost = method === "POST";
  if (isPost && !sameOrigin(request, env)) return html(signInPage({ error: "Request blocked." }), 403);

  if (pathname === "/auth/logout" && isPost) {
    await destroySession(request, env);
    return redirect("/", 303, { "set-cookie": clearSessionCookie() });
  }
  if (!user) return method === "GET" ? redirect("/") : html(signInPage({ error: "Sign in first." }), 401);

  if (pathname === "/dashboard" && method === "GET") return renderDashboard(env, user, url);

  const reportMatch = /^\/reports\/([0-9a-f-]{36})$/.exec(pathname);
  if (reportMatch && method === "GET") {
    const report = await getReport(env, user.id, reportMatch[1]);
    if (!report) return html(signInPage({ error: "Report not found." }), 404);
    return html(reportPage(report));
  }

  if (pathname === "/tokens" && isPost) {
    if (!(await activeSubscription(env, user.id))) return renderDashboard(env, user, url, { flash: "Subscribe to create API tokens.", status: 402 });
    const form = await readForm(request);
    const created = await createApiToken(env, user.id, form.name);
    if (created.error) return renderDashboard(env, user, url, { flash: created.error, status: 400 });
    return renderDashboard(env, user, url, { newToken: created });
  }

  const revokeMatch = /^\/tokens\/([0-9a-f-]{36})\/revoke$/.exec(pathname);
  if (revokeMatch && isPost) {
    await revokeApiToken(env, user.id, revokeMatch[1]);
    return redirect("/dashboard");
  }

  if (pathname === "/settings" && isPost) {
    const form = await readForm(request);
    const on = form.email_summaries === "1" || form.email_summaries === true ? 1 : 0;
    await env.DB.prepare("UPDATE users SET email_summaries = ? WHERE id = ?").bind(on, user.id).run();
    return redirect("/dashboard");
  }

  if (pathname === "/billing/checkout" && isPost) {
    if (await activeSubscription(env, user.id)) return redirect("/dashboard");
    const form = await readForm(request);
    const result = await createCheckoutSession(env, user, form.plan);
    if (result.error) return renderDashboard(env, user, url, { flash: result.error, status: 400 });
    return redirect(result.url);
  }

  if (pathname === "/billing/portal" && isPost) {
    const result = await createPortalSession(env, user);
    if (result.error) return renderDashboard(env, user, url, { flash: result.error, status: 400 });
    return redirect(result.url);
  }

  return null;
}

async function handleApi(request, env, url) {
  const { pathname } = url;
  const method = request.method;

  if (pathname === "/api/v1/reports" && method === "POST") {
    // Uploads come from the CLI or desktop app with an API token only.
    const user = await tokenUser(request, env);
    if (!user) return errorJson(401, "A valid API token is required (Authorization: Bearer crp_...).");
    if (!(await activeSubscription(env, user.id))) return errorJson(402, "CloudRipper Pro subscription is not active.");
    try {
      const upload = parseUpload(await request.text());
      const stored = await storeReport(env, user.id, upload);
      return json({ ok: true, report: stored }, 201);
    } catch (err) {
      if (err instanceof UploadError) return errorJson(err.status, err.message);
      throw err;
    }
  }

  // Read endpoints accept an API token or a browser session. Past reports stay
  // readable by their owner even if the subscription lapses.
  const user = await requestUser(request, env);
  if (!user) return errorJson(401, "Sign in or send an API token.");
  if (method !== "GET") return errorJson(405, "Method not allowed.");

  if (pathname === "/api/v1/me") {
    const sub = await activeSubscription(env, user.id);
    return json({ email: user.email, pro: Boolean(sub), plan: sub?.plan ?? null, current_period_end: sub?.current_period_end ?? null });
  }
  if (pathname === "/api/v1/accounts") return json({ accounts: await listAccounts(env, user.id) });
  if (pathname === "/api/v1/reports") {
    const kind = url.searchParams.get("kind");
    if (kind && !KINDS.includes(kind)) return errorJson(400, `kind must be one of ${KINDS.join(", ")}.`);
    const before = url.searchParams.get("before");
    const reports = await listReports(env, user.id, {
      accountId: url.searchParams.get("account"),
      kind,
      limit: url.searchParams.get("limit") ?? 50,
      before: before ? Number.parseInt(before, 10) || null : null,
    });
    return json({ reports });
  }
  const match = /^\/api\/v1\/reports\/([0-9a-f-]{36})$/.exec(pathname);
  if (match) {
    const report = await getReport(env, user.id, match[1]);
    return report ? json(report) : errorJson(404, "Report not found.");
  }
  return errorJson(404, "Not found.");
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    try {
      if (url.pathname.startsWith("/api/")) return await handleApi(request, env, url);
      const response = await handlePage(request, env, url);
      return response ?? html(signInPage({ error: "Page not found." }), 404);
    } catch (err) {
      console.error("unhandled", err?.stack ?? err);
      return url.pathname.startsWith("/api/") ? errorJson(500, "Internal error.") : html("Internal error.", 500);
    }
  },

  async scheduled(_event, env, ctx) {
    ctx.waitUntil(sendSummaries(env).then((r) => console.log("summaries", JSON.stringify(r))));
  },
};
