// Report upload, accounts, and history. Report JSON is stored in R2; D1 keeps
// the metadata the dashboard and email summaries need.

import { newId } from "./util.js";

export const MAX_REPORT_BYTES = 5 * 1024 * 1024;
export const KINDS = ["rip", "cost", "optimize"];
const LABEL_RE = /^[A-Za-z0-9][A-Za-z0-9 ._:@/-]{0,63}$/;

export class UploadError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

function num(value) {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

/** Infers the report kind from the shape of CLI JSON output. */
export function inferKind(report) {
  if (Array.isArray(report?.resources)) return "rip";
  if (Array.isArray(report?.Recommendations) || Array.isArray(report?.recommendations)) return "optimize";
  return null;
}

/**
 * Pulls summary numbers out of `rip`/`cost --output json` (scanDocument) or
 * `optimize --output json` (analysis uses Go field names; --apply uses snake_case).
 */
export function summarizeReport(kind, report) {
  const warnings = Array.isArray(report.warnings) ? report.warnings.length : 0;
  if (kind === "optimize") {
    const recs = Array.isArray(report.Recommendations) ? report.Recommendations : Array.isArray(report.recommendations) ? report.recommendations : [];
    const providers = new Set();
    for (const rec of recs) {
      const provider = rec?.Resource?.Provider ?? rec?.Resource?.provider;
      if (typeof provider === "string" && provider) providers.add(provider);
    }
    return {
      resource_count: null,
      estimated_total: num(report.TotalMonthlyCost ?? report.monthly_cost),
      actual_total: null,
      actual_available: null,
      potential_savings: num(report.PotentialSavings ?? report.potential_savings),
      recommendation_count: recs.length,
      warning_count: warnings,
      providers: [...providers].sort().join(",") || null,
    };
  }
  const resources = Array.isArray(report.resources) ? report.resources : [];
  const providers = new Set();
  for (const r of resources) if (typeof r?.provider === "string" && r.provider) providers.add(r.provider);
  return {
    resource_count: resources.length,
    estimated_total: num(report.estimated_total),
    actual_total: report.actual_available ? num(report.actual_total) : null,
    actual_available: typeof report.actual_available === "boolean" ? (report.actual_available ? 1 : 0) : null,
    potential_savings: null,
    recommendation_count: null,
    warning_count: warnings,
    providers: [...providers].sort().join(",") || null,
  };
}

/** Validates an upload envelope: { account, kind?, generated_at?, report }. */
export function parseUpload(bodyText) {
  if (bodyText.length > MAX_REPORT_BYTES) throw new UploadError(413, `Report is larger than ${MAX_REPORT_BYTES} bytes.`);
  let body;
  try {
    body = JSON.parse(bodyText);
  } catch {
    throw new UploadError(400, "Body must be JSON.");
  }
  if (!body || typeof body !== "object" || Array.isArray(body)) throw new UploadError(400, "Body must be a JSON object.");
  const label = typeof body.account === "string" ? body.account.trim() : "";
  if (!LABEL_RE.test(label)) {
    throw new UploadError(400, "account must be 1-64 characters: letters, digits, space, and . _ : @ / -, starting with a letter or digit.");
  }
  const report = body.report;
  if (!report || typeof report !== "object" || Array.isArray(report)) throw new UploadError(400, "report must be the JSON object printed by --output json.");
  const kind = body.kind ?? inferKind(report);
  if (!KINDS.includes(kind)) throw new UploadError(400, `kind must be one of ${KINDS.join(", ")}.`);
  if (kind === "optimize" ? inferKind(report) !== "optimize" : !Array.isArray(report.resources)) {
    throw new UploadError(400, `report does not look like \`cloudripper ${kind} --output json\` output.`);
  }
  let generatedAt = null;
  if (body.generated_at !== undefined && body.generated_at !== null) {
    const t = Date.parse(body.generated_at);
    if (Number.isNaN(t)) throw new UploadError(400, "generated_at must be an RFC 3339 timestamp.");
    generatedAt = t;
  }
  return { label, kind, report, generatedAt };
}

async function ensureAccount(env, userId, label, now) {
  await env.DB.prepare("INSERT INTO cloud_accounts (id, user_id, label, created_at) VALUES (?, ?, ?, ?) ON CONFLICT (user_id, label) DO NOTHING")
    .bind(newId(), userId, label, now)
    .run();
  return env.DB.prepare("SELECT * FROM cloud_accounts WHERE user_id = ? AND label = ?").bind(userId, label).first();
}

export async function storeReport(env, userId, upload, now = Date.now()) {
  const account = await ensureAccount(env, userId, upload.label, now);
  const id = newId();
  const key = `reports/${userId}/${account.id}/${id}.json`;
  const body = JSON.stringify(upload.report);
  const summary = summarizeReport(upload.kind, upload.report);
  await env.REPORTS.put(key, body, { httpMetadata: { contentType: "application/json" } });
  await env.DB.batch([
    env.DB.prepare(
      `INSERT INTO scan_reports (id, user_id, account_id, kind, source, r2_key, size_bytes, resource_count, estimated_total,
        actual_total, actual_available, potential_savings, recommendation_count, warning_count, providers, generated_at, created_at)
       VALUES (?, ?, ?, ?, 'upload', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    ).bind(
      id,
      userId,
      account.id,
      upload.kind,
      key,
      new TextEncoder().encode(body).length,
      summary.resource_count,
      summary.estimated_total,
      summary.actual_total,
      summary.actual_available,
      summary.potential_savings,
      summary.recommendation_count,
      summary.warning_count,
      summary.providers,
      upload.generatedAt,
      now,
    ),
    env.DB.prepare("UPDATE cloud_accounts SET last_report_at = ? WHERE id = ?").bind(now, account.id),
  ]);
  return { id, account: { id: account.id, label: account.label }, kind: upload.kind, created_at: now, ...summary };
}

export async function listAccounts(env, userId) {
  const { results } = await env.DB.prepare(
    `SELECT a.id, a.label, a.created_at, a.last_report_at,
       (SELECT COUNT(*) FROM scan_reports r WHERE r.account_id = a.id) AS report_count
     FROM cloud_accounts a WHERE a.user_id = ? ORDER BY a.label`,
  )
    .bind(userId)
    .all();
  return results;
}

const REPORT_COLUMNS = `r.id, r.account_id, a.label AS account_label, r.kind, r.source, r.size_bytes, r.resource_count,
  r.estimated_total, r.actual_total, r.actual_available, r.potential_savings, r.recommendation_count,
  r.warning_count, r.providers, r.generated_at, r.created_at`;

export async function listReports(env, userId, { accountId = null, kind = null, limit = 50, before = null } = {}) {
  const where = ["r.user_id = ?"];
  const args = [userId];
  if (accountId) {
    where.push("r.account_id = ?");
    args.push(accountId);
  }
  if (kind) {
    where.push("r.kind = ?");
    args.push(kind);
  }
  if (before) {
    where.push("r.created_at < ?");
    args.push(before);
  }
  const n = Math.min(Math.max(Number.parseInt(limit, 10) || 50, 1), 200);
  const { results } = await env.DB.prepare(
    `SELECT ${REPORT_COLUMNS} FROM scan_reports r JOIN cloud_accounts a ON a.id = r.account_id
     WHERE ${where.join(" AND ")} ORDER BY r.created_at DESC LIMIT ?`,
  )
    .bind(...args, n)
    .all();
  return results;
}

export async function getReport(env, userId, reportId) {
  const meta = await env.DB.prepare(
    `SELECT ${REPORT_COLUMNS}, r.r2_key FROM scan_reports r JOIN cloud_accounts a ON a.id = r.account_id WHERE r.id = ? AND r.user_id = ?`,
  )
    .bind(reportId, userId)
    .first();
  if (!meta) return null;
  const object = await env.REPORTS.get(meta.r2_key);
  const { r2_key: _key, ...rest } = meta;
  return { ...rest, report: object ? await object.json() : null };
}
