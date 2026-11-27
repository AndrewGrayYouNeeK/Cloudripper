// Server-rendered HTML for sign-in and the minimal dashboard. Every dynamic
// value goes through escapeHtml.

import { escapeHtml as e, formatTime, formatUsd } from "./util.js";

const STYLE = `body{font-family:system-ui,-apple-system,Segoe UI,sans-serif;max-width:960px;margin:0 auto;padding:24px;color:#111;background:#fafafa}
h1{font-size:1.5rem}h2{font-size:1.15rem;margin-top:2rem}table{border-collapse:collapse;width:100%;background:#fff}
th,td{border:1px solid #ddd;padding:6px 8px;text-align:left;font-size:.9rem;vertical-align:top}th{background:#f0f0f0}
form.inline{display:inline}input,button,select{font:inherit;padding:6px 10px}button{cursor:pointer}
.note{background:#fff8e1;border:1px solid #f0d58c;padding:10px;border-radius:6px}.ok{background:#e8f5e9;border-color:#a5d6a7}
code,pre{background:#eee;padding:2px 4px;border-radius:4px}pre{padding:10px;overflow:auto}header{display:flex;justify-content:space-between;align-items:center}`;

export function layout(title, body) {
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex"><title>${e(title)}</title><style>${STYLE}</style></head><body>${body}</body></html>`;
}

export function signInPage({ message = "", error = "" } = {}) {
  return layout(
    "Sign in · CloudRipper Pro",
    `<h1>CloudRipper Pro</h1>
${message ? `<p class="note ok">${e(message)}</p>` : ""}${error ? `<p class="note">${e(error)}</p>` : ""}
<form method="post" action="/auth/magic-link">
<label>Email <input type="email" name="email" required maxlength="254" autocomplete="email"></label>
<button type="submit">Email me a sign-in link</button></form>
<!-- TODO(stage 2): "Continue with GitHub" and "Continue with Google" buttons. -->`,
  );
}

export function verifyPage(token) {
  return layout(
    "Confirm sign-in · CloudRipper Pro",
    `<h1>Confirm sign-in</h1><p>Press the button to finish signing in to CloudRipper Pro.</p>
<form method="post" action="/auth/verify"><input type="hidden" name="token" value="${e(token)}"><button type="submit">Sign in</button></form>`,
  );
}

function subscriptionSection(sub, plans, lastSub) {
  if (sub) {
    const renew = sub.cancel_at_period_end ? "Ends" : "Renews";
    return `<p>Status: <strong>${e(sub.status)}</strong>${sub.plan ? ` · ${e(sub.plan)}` : ""}${sub.current_period_end ? ` · ${renew} ${e(formatTime(sub.current_period_end))}` : ""}</p>
<form method="post" action="/billing/portal"><button type="submit">Manage billing</button></form>`;
  }
  const lapsed = lastSub ? `<p class="note">Your subscription is ${e(lastSub.status)}. Uploads are paused until it is active again.</p>` : "";
  if (plans.length === 0) return `${lapsed}<p class="note">Checkout is not configured yet.</p>`;
  const buttons = plans
    .map((p) => `<form class="inline" method="post" action="/billing/checkout"><input type="hidden" name="plan" value="${e(p.id)}"><button type="submit">Subscribe · ${e(p.label)}</button></form>`)
    .join(" ");
  return `${lapsed}<p>Pro is $29/month${plans.some((p) => p.id === "annual") ? ", or billed annually" : ""}. Uploads, API tokens, and email summaries need an active subscription.</p>${buttons}`;
}

export function dashboardPage({ user, sub, lastSub, plans, accounts, reports, tokens, selectedAccount, newToken, flash, origin }) {
  const accountRows = accounts.length
    ? accounts
        .map(
          (a) => `<tr><td><a href="/dashboard?account=${encodeURIComponent(a.id)}">${e(a.label)}</a></td><td>${e(a.report_count)}</td><td>${e(formatTime(a.last_report_at))}</td></tr>`,
        )
        .join("")
    : `<tr><td colspan="3">No accounts yet. Upload a report to create one.</td></tr>`;
  const reportRows = reports.length
    ? reports
        .map(
          (r) => `<tr><td><a href="/reports/${encodeURIComponent(r.id)}">${e(formatTime(r.created_at))}</a></td><td>${e(r.account_label)}</td><td>${e(r.kind)}</td>
<td>${r.kind === "optimize" ? `${e(r.recommendation_count ?? 0)} recs` : e(r.resource_count ?? "")}</td>
<td>${formatUsd(r.estimated_total)}</td><td>${r.kind === "optimize" ? `saves ${formatUsd(r.potential_savings)}` : formatUsd(r.actual_total)}</td><td>${e(r.providers ?? "")}</td></tr>`,
        )
        .join("")
    : `<tr><td colspan="7">No reports yet.</td></tr>`;
  const tokenRows = tokens.length
    ? tokens
        .map(
          (t) => `<tr><td>${e(t.name)}</td><td><code>${e(t.prefix)}…</code></td><td>${e(formatTime(t.created_at))}</td><td>${e(formatTime(t.last_used_at))}</td>
<td><form class="inline" method="post" action="/tokens/${encodeURIComponent(t.id)}/revoke"><button type="submit">Revoke</button></form></td></tr>`,
        )
        .join("")
    : `<tr><td colspan="5">No tokens.</td></tr>`;
  const tokenNote = newToken
    ? `<div class="note ok"><p>New token <strong>${e(newToken.name)}</strong>. Copy it now; it is not shown again.</p><pre>${e(newToken.token)}</pre></div>`
    : "";
  return layout(
    "Dashboard · CloudRipper Pro",
    `<header><h1>CloudRipper Pro</h1><div>${e(user.email)} <form class="inline" method="post" action="/auth/logout"><button type="submit">Sign out</button></form></div></header>
${flash ? `<p class="note">${e(flash)}</p>` : ""}
<h2>Subscription</h2>${subscriptionSection(sub, plans, lastSub)}
<h2>Cloud accounts</h2><table><tr><th>Account</th><th>Reports</th><th>Last upload</th></tr>${accountRows}</table>
<h2>History${selectedAccount ? ` · ${e(selectedAccount.label)} <a href="/dashboard">(all)</a>` : ""}</h2>
<table><tr><th>Uploaded</th><th>Account</th><th>Kind</th><th>Resources</th><th>Estimated</th><th>Actual 30d / savings</th><th>Providers</th></tr>${reportRows}</table>
<h2>API tokens</h2>${tokenNote}
<table><tr><th>Name</th><th>Prefix</th><th>Created</th><th>Last used</th><th></th></tr>${tokenRows}</table>
${sub ? `<form method="post" action="/tokens"><input name="name" placeholder="laptop" maxlength="60" required> <button type="submit">Create token</button></form>` : "<p>Subscribe to create tokens.</p>"}
<h2>Upload from the CLI</h2>
<p>Your cloud credentials stay on your machine. The CLI runs the scan locally and uploads only the JSON report.</p>
<pre>export CLOUDRIPPER_PRO_URL=${e(origin)}
export CLOUDRIPPER_PRO_TOKEN=crp_...
cloudripper upload --account prod-aws            # scan now and upload
cloudripper upload --account prod-aws --every 6h # keep scanning on this machine</pre>
<h2>Email summaries</h2>
<form method="post" action="/settings"><label><input type="checkbox" name="email_summaries" value="1"${user.email_summaries ? " checked" : ""}> Send me a weekly summary</label> <button type="submit">Save</button></form>`,
  );
}

export function reportPage(report) {
  const data = report.report ?? {};
  const warnings = Array.isArray(data.warnings) && data.warnings.length ? `<h2>Warnings</h2><ul>${data.warnings.map((w) => `<li>${e(w)}</li>`).join("")}</ul>` : "";
  let table = "";
  const LIMIT = 500;
  if (report.kind === "optimize") {
    const recs = (Array.isArray(data.Recommendations) ? data.Recommendations : data.recommendations) ?? [];
    table = `<h2>Recommendations (${recs.length})</h2><table><tr><th>Provider</th><th>Resource</th><th>Action</th><th>Savings</th><th>Reason</th></tr>${recs
      .slice(0, LIMIT)
      .map((r) => {
        const res = r.Resource ?? {};
        return `<tr><td>${e(res.Provider)}</td><td>${e(res.Name || res.ID)}</td><td>${e(r.Action)}</td><td>${formatUsd(r.Savings)}</td><td>${e(r.Reason)}</td></tr>`;
      })
      .join("")}</table>`;
  } else {
    const rows = Array.isArray(data.resources) ? data.resources : [];
    table = `<h2>Resources (${rows.length}${rows.length > LIMIT ? `, first ${LIMIT} shown` : ""})</h2><table><tr><th>Provider</th><th>Type</th><th>Name</th><th>Region</th><th>Status</th><th>Cost/mo</th></tr>${rows
      .slice(0, LIMIT)
      .map((r) => `<tr><td>${e(r.provider)}</td><td>${e(r.type)}</td><td>${e(r.name)}</td><td>${e(r.region)}</td><td>${e(r.status)}</td><td>${r.cost_usd === null ? "unpriced" : formatUsd(r.cost_usd)}</td></tr>`)
      .join("")}</table>`;
  }
  return layout(
    "Report · CloudRipper Pro",
    `<p><a href="/dashboard">← Dashboard</a> · <a href="/api/v1/reports/${encodeURIComponent(report.id)}">JSON</a></p>
<h1>${e(report.account_label)} · ${e(report.kind)}</h1>
<p>Uploaded ${e(formatTime(report.created_at))}${report.generated_at ? ` · generated ${e(formatTime(report.generated_at))}` : ""}</p>
<p>Estimated: ${formatUsd(report.estimated_total)} · Actual 30 days: ${formatUsd(report.actual_total)}${report.kind === "optimize" ? ` · Potential savings: ${formatUsd(report.potential_savings)}` : ""}</p>
${report.report ? "" : `<p class="note">The stored report file is missing.</p>`}${warnings}${table}`,
  );
}
