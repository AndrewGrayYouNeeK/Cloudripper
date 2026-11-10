import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const html = readFileSync(new URL("../public/index.html", import.meta.url), "utf8");

test("landing copy matches the CLI", () => {
  for (const needle of [
    "AWS, GCP, Azure and Cloudflare",
    "Runs on your machine",
    "Your cloud data is never sent to us",
    "Billing Read",
    "unpriced",
    "scheduled scans",
    "multi-account",
    "team invites",
    "--apply",
    "CHAOS_ENDPOINT",
  ]) {
    assert.ok(html.includes(needle), `missing ${needle}`);
  }
  for (const banned of [
    "not estimates",
    "no data leaving your machine",
    "we'll email you once",
    "No spam, one email",
    "Real savings",
  ]) {
    assert.equal(html.toLowerCase().includes(banned.toLowerCase()), false, `still says ${banned}`);
  }
});
