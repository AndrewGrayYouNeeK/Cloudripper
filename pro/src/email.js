// Outgoing mail through the Cloudflare Email Sending `send_email` binding (env.EMAIL).
// EMAIL_FROM must be an address on a domain onboarded to Email Sending.

import { escapeHtml } from "./util.js";

export async function sendEmail(env, { to, subject, text, html }) {
  if (!env.EMAIL || typeof env.EMAIL.send !== "function") throw new Error("EMAIL binding is not configured");
  if (!env.EMAIL_FROM) throw new Error("EMAIL_FROM is not configured");
  return env.EMAIL.send({ to, from: { email: env.EMAIL_FROM, name: "CloudRipper Pro" }, subject, text, html });
}

export async function sendMagicLinkEmail(env, to, link) {
  const text = [
    "Sign in to CloudRipper Pro:",
    "",
    link,
    "",
    "This link works once and expires in 15 minutes.",
    "If you did not ask for it, you can ignore this email.",
  ].join("\n");
  const html = `<p>Sign in to CloudRipper Pro:</p>
<p><a href="${escapeHtml(link)}">Sign in</a></p>
<p>This link works once and expires in 15 minutes. If you did not ask for it, you can ignore this email.</p>`;
  return sendEmail(env, { to, subject: "Your CloudRipper Pro sign-in link", text, html });
}
