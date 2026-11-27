// Periodic email summaries for active Pro subscribers, run from the cron trigger.
// Each account's latest report is compared with the report before it.

import { ACTIVE_STATUSES } from "./billing.js";
import { sendEmail } from "./email.js";
import { appOrigin, escapeHtml, formatTime, formatUsd } from "./util.js";

export const SUMMARY_MIN_INTERVAL_MS = 6 * 24 * 60 * 60 * 1000;

function delta(latest, previous) {
  if (latest === null || latest === undefined || previous === null || previous === undefined) return null;
  return Math.round((latest - previous) * 100) / 100;
}

function signed(value) {
  if (value === null) return "n/a";
  return `${value >= 0 ? "+" : "-"}${formatUsd(Math.abs(value))}`;
}

/** Builds per-account rows from the two most recent rip/cost reports. */
export async function buildSummary(env, userId) {
  const { results: accounts } = await env.DB.prepare("SELECT id, label FROM cloud_accounts WHERE user_id = ? ORDER BY label")
    .bind(userId)
    .all();
  const rows = [];
  for (const account of accounts) {
    const { results } = await env.DB.prepare(
      `SELECT estimated_total, actual_total, resource_count, created_at FROM scan_reports
       WHERE account_id = ? AND kind IN ('rip', 'cost') ORDER BY created_at DESC LIMIT 2`,
    )
      .bind(account.id)
      .all();
    if (results.length === 0) continue;
    const [latest, previous] = results;
    rows.push({
      label: account.label,
      scanned_at: latest.created_at,
      resource_count: latest.resource_count,
      estimated_total: latest.estimated_total,
      actual_total: latest.actual_total,
      estimated_change: previous ? delta(latest.estimated_total, previous.estimated_total) : null,
      actual_change: previous ? delta(latest.actual_total, previous.actual_total) : null,
    });
  }
  return rows;
}

export function renderSummary(env, rows) {
  const link = `${appOrigin(env)}/dashboard`;
  const textLines = ["Your CloudRipper Pro summary", ""];
  for (const r of rows) {
    textLines.push(
      `${r.label}: ${r.resource_count ?? "?"} resources, estimated ${formatUsd(r.estimated_total)} (${signed(r.estimated_change)}), ` +
        `actual 30-day ${formatUsd(r.actual_total)} (${signed(r.actual_change)}), last upload ${formatTime(r.scanned_at)}`,
    );
  }
  textLines.push("", `Dashboard: ${link}`, "Turn these emails off on the dashboard.");
  const tableRows = rows
    .map(
      (r) => `<tr><td>${escapeHtml(r.label)}</td><td>${escapeHtml(r.resource_count ?? "?")}</td><td>${formatUsd(r.estimated_total)} (${signed(r.estimated_change)})</td><td>${formatUsd(r.actual_total)} (${signed(r.actual_change)})</td><td>${escapeHtml(formatTime(r.scanned_at))}</td></tr>`,
    )
    .join("");
  const htmlBody = `<p>Your CloudRipper Pro summary</p>
<table border="1" cellpadding="6" cellspacing="0"><tr><th>Account</th><th>Resources</th><th>Estimated</th><th>Actual (30 days)</th><th>Last upload</th></tr>${tableRows}</table>
<p><a href="${escapeHtml(link)}">Open the dashboard</a>. Turn these emails off on the dashboard.</p>`;
  return { subject: "Your CloudRipper Pro summary", text: textLines.join("\n"), html: htmlBody };
}

/** Sends one summary per eligible subscriber. Returns counts for logs/tests. */
export async function sendSummaries(env, now = Date.now()) {
  const { results: users } = await env.DB.prepare(
    `SELECT DISTINCT u.id, u.email FROM users u JOIN subscriptions s ON s.user_id = u.id
     WHERE u.email_summaries = 1 AND s.status IN (${ACTIVE_STATUSES.map(() => "?").join(", ")})
       AND (u.last_summary_at IS NULL OR u.last_summary_at < ?)`,
  )
    .bind(...ACTIVE_STATUSES, now - SUMMARY_MIN_INTERVAL_MS)
    .all();
  let sent = 0;
  let skipped = 0;
  let failed = 0;
  for (const user of users) {
    const rows = await buildSummary(env, user.id);
    if (rows.length === 0) {
      skipped++;
      continue;
    }
    try {
      await sendEmail(env, { to: user.email, ...renderSummary(env, rows) });
      await env.DB.prepare("UPDATE users SET last_summary_at = ? WHERE id = ?").bind(now, user.id).run();
      sent++;
    } catch (err) {
      failed++;
      console.error("summary email failed", user.id, err?.message);
    }
  }
  return { sent, skipped, failed };
}
