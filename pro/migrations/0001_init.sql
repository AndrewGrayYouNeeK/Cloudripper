-- CloudRipper Pro, stage 1 schema. All timestamps are Unix milliseconds (UTC).

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  stripe_customer_id TEXT UNIQUE,
  email_summaries INTEGER NOT NULL DEFAULT 1,
  last_summary_at INTEGER
);

-- One-time sign-in links. Only a SHA-256 hash of the token is stored.
CREATE TABLE magic_links (
  token_hash TEXT PRIMARY KEY,
  email TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at INTEGER
);
CREATE INDEX magic_links_email_created ON magic_links (email, created_at);

-- Browser sessions. Only a SHA-256 hash of the cookie value is stored.
CREATE TABLE sessions (
  id_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE INDEX sessions_user ON sessions (user_id);

-- A cloud account is a label the user picks (for example "prod-aws").
-- One user can have many. Stage 2 adds optional stored read-only credentials.
CREATE TABLE cloud_accounts (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_report_at INTEGER,
  UNIQUE (user_id, label)
);

-- Report metadata. The full JSON report lives in R2 at r2_key.
CREATE TABLE scan_reports (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  account_id TEXT NOT NULL REFERENCES cloud_accounts (id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('rip', 'cost', 'optimize')),
  source TEXT NOT NULL DEFAULT 'upload' CHECK (source IN ('upload', 'hosted')),
  r2_key TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  resource_count INTEGER,
  estimated_total REAL,
  actual_total REAL,
  actual_available INTEGER,
  potential_savings REAL,
  recommendation_count INTEGER,
  warning_count INTEGER NOT NULL DEFAULT 0,
  providers TEXT,
  generated_at INTEGER,
  created_at INTEGER NOT NULL
);
CREATE INDEX scan_reports_account_created ON scan_reports (account_id, created_at DESC);
CREATE INDEX scan_reports_user_created ON scan_reports (user_id, created_at DESC);

-- API tokens for the CLI and desktop app. Only a SHA-256 hash is stored.
CREATE TABLE api_tokens (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  prefix TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER,
  revoked_at INTEGER
);
CREATE INDEX api_tokens_user ON api_tokens (user_id);

-- Mirror of Stripe subscriptions, written only by the verified webhook.
CREATE TABLE subscriptions (
  stripe_subscription_id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  stripe_customer_id TEXT NOT NULL,
  status TEXT NOT NULL,
  plan TEXT,
  price_id TEXT,
  current_period_end INTEGER,
  cancel_at_period_end INTEGER NOT NULL DEFAULT 0,
  -- Stripe event.created (seconds) of the last applied event; older events are ignored.
  event_created INTEGER,
  updated_at INTEGER NOT NULL
);
CREATE INDEX subscriptions_user ON subscriptions (user_id);

-- Processed Stripe event ids, so retried deliveries are applied once.
CREATE TABLE stripe_events (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  received_at INTEGER NOT NULL
);
