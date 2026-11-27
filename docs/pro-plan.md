# CloudRipper Pro launch plan

First written for Andrew's review. Facts checked against `https://github.com/AndrewGrayYouNeeK/Cloudripper` at main `55d6999` (2026-11-15, America/Chicago). Updated 2026-11-26 with Andrew's decisions and the stage 1 build (`pro/` and `cloudripper upload`, in a pull request, not deployed).

## Decisions (Andrew, 2026-11-26)

| # | Question | Decision |
| --- | --- | --- |
| 1 | How do scans reach the dashboard? | **Hybrid.** Default: the local CLI or desktop app scans, credentials stay on the machine, and only JSON reports are uploaded. Opt-in: stored read-only credentials, encrypted at rest, for hosted scans. |
| 2 | Where does the backend live? | **Cloudflare:** Workers, D1, and R2. Pro is its own Worker (`cloudripper-pro`, in `pro/`). `cloudripper-site` and its `WAITLIST` KV stay as they are. |
| 3 | How do people sign in? | **Both:** email magic link, and OAuth with GitHub and Google. |
| 4 | Is Pro one cloud account? | **No: multiple cloud accounts per user.** |
| 5 | Price shape | **$29/month plus an annual option.** The annual price is a Stripe config value Andrew sets; no amount is chosen here. |
| 6 | Mailer | **Cloudflare Email Sending**, for magic links and summary emails. |
| 7 | Waitlist email and site call to action | **Still needs a separate, explicit OK.** Nothing has been sent or changed. |
| 8 | Local scheduler OK? | **Yes.** Scheduled scans may run on the user's machine and upload results. |

## Build staging

Andrew approved building on the box and opening a GitHub PR, staged:

- **Stage 1 (first PR):** D1 schema (users, sessions, magic links, cloud accounts, scan reports, API tokens, subscriptions, Stripe events); magic-link sign-in; report upload, list, and history across multiple accounts with R2 storage; API tokens; a minimal dashboard; Stripe Checkout and Customer Portal with webhook gating (monthly price plus optional annual price from config); weekly email summaries through Cloudflare Email Sending; and the CLI `cloudripper upload` command, with `--every` as the local scheduler. Not deployed. `pro/README.md` lists the resources and secrets to create first.
- **Stage 2 (follow-up):** GitHub and Google OAuth; opt-in stored read-only credentials, encrypted at rest, plus the hosted scan job. Also: an upload button in the desktop app, if wanted.

## What exists today

The product is a free MIT-licensed local Go CLI for AWS, GCP, Azure, and Cloudflare: inventory, estimated vs actual spend, optimizations, forgotten bills, scheduling, scaling, watch alerts, and bounded chaos. It runs on the user's machine with their credentials. The README and site FAQ say there is no hosted backend and that cloud data is never sent to us. A read-only Wails app in `desktop/` shows the same local scans and exports HTML, CSV, and JSON. It does not apply changes, scale, enforce schedules, or run chaos.

https://cloudripper.youneek.dev is Cloudflare Worker `cloudripper-site` (`site/`). The only API is `POST /api/waitlist`. KV `WAITLIST` stores `{ email, joinedAt, interests?, interestNote? }`. Allowed interests: scheduled scans, multi-account, history, team invites, other. Notes are at most 200 characters. The form and Worker do not send email. The repo has no waitlist export route, auth, checkout, or dashboard backend.

Published pricing: free CLI; Pro at $29/mo, "Coming soon" (hosted dashboard, scheduled scans and history, email summaries); Team/Business from $199/mo (multiple cloud accounts, SSO, alerts, priority support; contact `andrewgray@usedartifacts.com`). The FAQ says paid plans are only for the optional hosted dashboard, and that there is no Pro date yet.

CLI `schedule` stops or starts tagged compute on a pull-based check. That is not the Pro "scheduled scans" feature.

## v1: smallest real Pro

Ship only what the site already lists for Pro, for one paying user. Per decision 4, that user can have several cloud accounts:

1. An account and a $29/mo Stripe subscription, or the annual price, with the Customer Portal to update the card, switch plans, or cancel.
2. A hosted dashboard of the latest rip, cost, and optimize results, plus scan history.
3. A way to get those results into the dashboard on a schedule (decision 1).
4. A periodic email summary: latest totals, change since last scan, link to the dashboard.
5. Site updates: swap "Coming soon / Join the waitlist" for checkout; revise the FAQ that says data never leaves the machine, that the site has no mailer, and that there is no launch date.
6. A one-time waitlist conversion email after Andrew approves the list and the copy.

Out of v1: Team features (SSO, team invites, alerts, priority support; multiple accounts moved into Pro by decision 4), write credentials, hosted `optimize --apply` / `scale` / chaos, hosted `find-bills` (needs mail access), and a phone app. The free CLI and desktop app stay free and local.

## Architecture

Two ways to honor "a hosted dashboard so you don't have to run it by hand." Both collide with the current privacy promise.

**A. Upload results (recommended for v1).** The CLI or desktop keeps credentials on the machine, runs `rip` / `cost` / `optimize` as today, and POSTs the existing JSON (`--output json`) to a Pro API. History is those snapshots. "Scheduled scans" is a local helper: cron, or a desktop timer. The host never holds cloud keys. Tradeoff: a closed laptop skips a run, which is weaker than "don't run it by hand," but it only needs an opt-in FAQ change ("Pro uploads the reports you choose to send").

**B. Hold read-only cloud credentials and scan in our cloud.** The dashboard stores read-only AWS/GCP/Azure/Cloudflare keys and a job runs inventory on a schedule, even when the user is offline. This matches the marketing line and is a much larger trust and security change (encryption at rest, rotation, least-privilege docs, incident response). Write access must stay out.

**Decided: hybrid.** A is the default and ships in stage 1. B ships in stage 2 as an opt-in, with read-only keys encrypted at rest.

Suggested stack, next to the existing Worker: Cloudflare Workers for the API and dashboard, D1 for users / subscriptions / scan metadata, R2 for report JSON, Stripe for billing, and Cloudflare Email Sending as the mailer (decision 6). Supabase Auth + Postgres is a reasonable alternative if login and a relational dashboard should ship faster. Do not use WAITLIST KV as the product database.

Auth: email magic link in stage 1, plus Google and GitHub OAuth in stage 2 (decision 3). SSO waits for Team. Access is a valid session plus an active Stripe subscription. Webhooks (`checkout.session.completed`, `customer.subscription.updated` / `deleted`) grant or revoke Pro. No custom card forms.

A scheduled Worker sends the digest to active subscribers. The same mailer sends magic links (if used) and the one-time waitlist announcement. Do not email the waitlist until Andrew signs off on recipients, subject, and body. Conversion is a one-off, reviewed WAITLIST KV dump (no export route exists; not a public API), then a checkout link, then the site CTA points at Stripe. A founding discount is optional (decision 5). Team stays a mailto.

## Phased milestones (rough, one person, assumes model A)

| Phase | Work | Rough effort |
| --- | --- | --- |
| 0 | Andrew locks the decisions below | — |
| 1 | Auth, Stripe product/price, Checkout, Customer Portal, webhooks, "Pro active" flag. Site CTA can stay hidden until launch. | 1–2 weeks |
| 2 | Authenticated upload API, dashboard for latest rip/cost/optimize, scan history. Optional CLI/desktop "upload to Pro." | ~2 weeks |
| 3 | Local scheduled upload, email digest, basic retry. | 1–2 weeks |
| 4 | Privacy/ToS/FAQ updates, waitlist email, public CTA swap. Needs an explicit launch OK. | A few days |
| Later | Optional hosted read-only keys (model B), Team/Business, multi-account, SSO, alerts. | After v1 is collecting fees |

About 4–6 weeks to a paid v1 if decisions are prompt and scope stays at the list above.

## Original open decisions (resolved above, kept for the record)

1. **How do scans reach the dashboard?** (A) Upload local CLI/desktop JSON; credentials stay on the machine. (B) Store the user's read-only cloud credentials and scan in our cloud. (C) A now, B later. Recommendation: C, with A as v1.
2. **Where does the backend live?** (A) Cloudflare Workers + D1 + R2, next to `cloudripper-site`. (B) Supabase Auth + Postgres. (C) Supabase for auth/data, Cloudflare for the site and jobs.
3. **How do people sign in?** (A) Email magic link. (B) Google and/or GitHub OAuth. (C) Both.
4. **Is v1 one cloud account per subscriber?** The site puts "multiple cloud accounts" in Team. (A) Yes, one account in Pro. (B) Allow several accounts in Pro anyway.
5. **Price shape at checkout?** Public price is $29/mo. (A) Monthly only. (B) Also an annual option (price TBD). (C) A waitlist founding discount (amount and duration TBD).
6. **Which transactional mailer?** The site has none. Pick one before phase 3 (for example Resend, Postmark, or SES). This also unlocks waitlist mail.
7. **When may we email the waitlist and flip the site from waitlist to checkout?** Needs a separate, explicit OK, including the export review and the email copy. Approving this plan does not grant that.
8. **For model A, is a local scheduler acceptable in v1?** (A) Yes: cron or a desktop timer; a closed laptop skips a run. (B) No: v1 must scan while the user is offline, which means model B.

## Risks

- The live FAQ and README say cloud data never comes to us. Update that copy before the first upload or stored key.
- Model B creates a credentials incident class the project does not have today. If chosen: read-only keys only, encrypted at rest, with rotation and a public description of what is stored.
- Model A's scheduled scans fail when the agent is off, so emails go stale. Say this clearly in the UI.
- Waitlist interests include multi-account and team invites, which the site sells as Team, not Pro. Conversion mail should not promise those in the $29 plan.
- The site Worker has no waitlist export, Terms, or privacy policy. Checkout should not go live without those, or without tax/business settings in Stripe (Andrew's call).
- Hosted scans (model B) would pay provider API costs, including AWS Cost Explorer at $0.01 per request as documented in the README. Upload-only v1 leaves that cost with the user.
- The MIT CLI can be used without Pro forever. Pro has to be worth $29/mo as convenience and history, not as a gate on features that already ship free.
