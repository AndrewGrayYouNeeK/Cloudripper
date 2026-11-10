# Changelog

## Unreleased

- `cloudripper demo` prints rip, cost, and optimize-style output from a fictional multi-cloud sample embedded in the binary. It does not use credentials or the network. Amounts are made up and are not real spend or real savings.
- `find-bills --sample` reads that same embedded card export as `docs/examples/sample-card-export.csv`, with no file and no network.
- `--output html` writes one self-contained report for commands that print rows, including `rip`, `cost`, `optimize`, and `find-bills`. Values are escaped. Unpriced costs are `n/a`. Missing actuals are `unavailable`, not `$0`.
- Table color is on only when stdout is a terminal. `--no-color` and a non-empty `NO_COLOR` turn it off. `json`, `csv`, and `html` are never colored.
- README lists the read-only cloud calls this code makes, and how many API requests each command sends. AWS Cost Explorer is cited at $0.01 per request for the primary billing view, from AWS's pricing page.

### Fixed

- Cloudflare Pages listing requests `per_page=10` and walks every page. The live `GET /accounts/{account_id}/pages/projects` API returns HTTP 400 for `per_page` 15, 20, 25, and 50.
- `cost --output json` uses the same snake_case resource schema as `rip --output json`. Unpriced rows, including Cloudflare inventory, have `cost_usd: null` instead of `0`. Priced rows from other providers stay numeric, including a real zero estimate.
- JSON monetary totals (`estimated_total`, `actual_total`, snapshot `total_usd`, and snapshot service and resource amounts) are rounded to the nearest cent.
- Cost summaries say `across 1 service` and `across N services`.
