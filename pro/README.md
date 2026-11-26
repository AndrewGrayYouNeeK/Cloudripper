# CloudRipper Pro (stage 1)

Source for the paid CloudRipper Pro service: a Cloudflare Worker (`cloudripper-pro`) with D1, R2, Cloudflare Email Sending, and Stripe. It is separate from the public site Worker in `../site` (`cloudripper-site` and its `WAITLIST` KV), which this directory does not touch.

**Not deployed.** Nothing here is live until the resources and secrets below exist and someone runs `wrangler deploy`.

## What stage 1 does

- **Sign-in by email magic link.** The link works once and expires after 15 minutes. Only a SHA-256 hash of the token is stored. Each address can request 5 links an hour. Opening the link shows a "Sign in" button, so mail scanners that prefetch links do not use it up. Sessions are `HttpOnly; Secure; SameSite=Lax` cookies that last 30 days, and only their hash is stored. Cookie-authenticated POSTs must carry this app's `Origin`.
- **Multiple cloud accounts per user.** An account is a label the user picks, such as `prod-aws`. The first upload with a new label creates it.
- **Report upload and history.** `POST /api/v1/reports` accepts the JSON that `cloudripper rip`, `cost`, or `optimize` print with `--output json`, up to 5 MiB. The full JSON goes to R2 at `reports/<user>/<account>/<report>.json`. D1 keeps the summary numbers: resource count, estimated and actual totals, potential savings, warning count, and providers.
- **API tokens** (`crp_...`). They are created on the dashboard and shown once. Only their hash is stored. They can be revoked. Uploads accept only a token, not a browser session.
- **Minimal dashboard.** It shows subscription status, accounts, history filtered by account, a report detail page, tokens, and the email-summary setting. Pages are server-rendered with every value escaped, under a strict CSP.
- **Stripe subscription gating.** Checkout (`mode=subscription`) uses `STRIPE_PRICE_MONTHLY`, plus `STRIPE_PRICE_ANNUAL` when it is set. The annual button stays hidden until the price ID is configured. The Customer Portal handles card changes, plan switches, and cancellation. The webhook verifies `Stripe-Signature` (HMAC-SHA256, 5-minute tolerance). Each event is applied once, and older events cannot overwrite newer subscription state. Pro means a subscription in `active` or `trialing`. Uploads, new tokens, and summary emails need Pro. Past reports stay readable by their owner after a subscription lapses.
- **Email summaries.** A weekly cron (Mondays 13:00 UTC) emails each Pro user who has uploaded reports and has summaries turned on. For each account it shows the latest rip or cost totals and the change since the previous report. Mail goes out through the `send_email` binding. The same binding sends magic links.

The local side is `cloudripper upload` in the Go CLI (see below).

## Not in stage 1 (stage 2 follow-up)

- GitHub and Google OAuth sign-in. There are `TODO(stage 2)` notes in `src/auth.js` and `src/pages.js`.
- Opt-in stored read-only cloud credentials, encrypted at rest, for hosted scans that run while the user's machine is off. The schema already allows `scan_reports.source = 'hosted'`. Credential storage, encryption keys, and the scan job are not built.

## HTTP routes

| Route | Auth | Purpose |
| --- | --- | --- |
| `GET /` | none | Sign-in form (redirects to `/dashboard` when signed in) |
| `POST /auth/magic-link` | none | Email a sign-in link |
| `GET /auth/verify?token=` | none | Confirm page with a sign-in button |
| `POST /auth/verify` | same origin | Use the token and set the session cookie |
| `POST /auth/logout` | session | Sign out |
| `GET /dashboard` | session | Dashboard (`?account=<id>` filters history) |
| `GET /reports/<id>` | session | Report detail |
| `POST /tokens`, `POST /tokens/<id>/revoke` | session + Pro (create) | API tokens |
| `POST /settings` | session | Email summaries on/off |
| `POST /billing/checkout` | session | Redirect to Stripe Checkout (`plan=monthly` or `annual`) |
| `POST /billing/portal` | session | Redirect to the Stripe Customer Portal |
| `POST /stripe/webhook` | Stripe signature | Subscription sync |
| `POST /api/v1/reports` | API token + Pro | Upload `{ account, kind?, generated_at?, report }` |
| `GET /api/v1/me` | token or session | Email and Pro status |
| `GET /api/v1/accounts` | token or session | Accounts with report counts |
| `GET /api/v1/reports?account=&kind=&limit=&before=` | token or session | History, newest first (`limit` up to 200) |
| `GET /api/v1/reports/<id>` | token or session | Metadata plus the full report JSON |

## Develop and test

```bash
cd pro
npm ci
npm test        # node --test; D1 is emulated with node:sqlite and the real migrations
npx wrangler d1 migrations apply cloudripper-pro --local
npx wrangler dev --var APP_URL:http://localhost:8787 --var EMAIL_FROM:pro@example.com
```

`wrangler dev` writes outgoing email to files under `.wrangler/tmp/email/` instead of sending it. Trigger the cron locally with `curl http://localhost:8787/cdn-cgi/local/scheduled`.

## Before the first deploy

Everything below is done by Andrew in the Cloudflare and Stripe dashboards or with Wrangler. None of it exists yet.

### Cloudflare (account `d74b0de637f4d9d9eb3efb9e13142784`, already in `wrangler.jsonc`)

1. **D1 database:** `npx wrangler d1 create cloudripper-pro`. Put the returned `database_id` in `wrangler.jsonc` in place of `REPLACE_WITH_D1_DATABASE_ID`, then run `npx wrangler d1 migrations apply cloudripper-pro --remote`.
2. **R2 bucket:** `npx wrangler r2 bucket create cloudripper-pro-reports`.
3. **Email Sending:** onboard a sending domain (`npx wrangler email sending enable <domain>`, or do it in the dashboard) and let its DNS records verify. Set `EMAIL_FROM` to an address on that domain.
4. **Hostname:** choose where Pro lives, for example a subdomain of `youneek.dev`. Add a `routes` entry with `custom_domain: true` (the same pattern the site uses), or keep `workers.dev`. Set `APP_URL` to that origin. Magic links, Stripe redirects, and the Origin check all use it.

### Stripe (test mode first)

5. Create a product for CloudRipper Pro with a recurring **$29/month** price, and put its price ID in `STRIPE_PRICE_MONTHLY`. To offer annual billing, create a recurring yearly price at whatever amount you choose and put its ID in `STRIPE_PRICE_ANNUAL`. Leave it empty to hide the option.
6. Configure the Customer Portal in the dashboard: allow payment-method updates and cancellation, and add the annual price if you want users to switch plans there.
7. Add a webhook endpoint at `<APP_URL>/stripe/webhook` with the events `checkout.session.completed`, `customer.subscription.created`, `customer.subscription.updated`, and `customer.subscription.deleted`.

### Secrets (`npx wrangler secret put <NAME>` from `pro/`)

| Secret | Value |
| --- | --- |
| `STRIPE_SECRET_KEY` | Stripe secret key (`sk_test_...` until launch) |
| `STRIPE_WEBHOOK_SECRET` | Signing secret of the webhook endpoint (`whsec_...`) |

### Vars (`vars` in `wrangler.jsonc`, not secret)

`APP_URL`, `EMAIL_FROM`, `STRIPE_PRICE_MONTHLY`, and `STRIPE_PRICE_ANNUAL` (optional).

### Before taking money

These are from the Pro plan and need Andrew's sign-off. This PR does none of them:

- Update the README, the site FAQ, and the privacy copy that say no cloud data leaves the machine. Pro uploads only the reports a user chooses to send.
- Write Terms and a privacy policy, and configure tax and business settings in Stripe.
- Change the site's call to action from the waitlist to checkout, and send any email to the waitlist.

## Uploading from the CLI

```bash
export CLOUDRIPPER_PRO_URL=https://<APP_URL host>
export CLOUDRIPPER_PRO_TOKEN=crp_...           # from the dashboard; read only from the environment
cloudripper upload --account prod-aws           # scan here and upload the rip report
cloudripper upload --account prod-aws --every 6h   # local scheduler: rescan and upload every 6h (min 15m)
cloudripper cost --output json > cost.json
cloudripper upload --account prod-aws --file cost.json --kind cost
```

`--every` keeps running in the foreground. Run it under cron, launchd, systemd, or Task Scheduler as a one-shot `cloudripper upload`, or leave it running. If the machine is off, that run is skipped. A bad token, an inactive subscription, or a rejected report stops the loop. Network and server errors are retried on the next tick. The endpoint must be `https` (plain `http` is allowed only for localhost).
