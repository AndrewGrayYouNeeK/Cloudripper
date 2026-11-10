# Changelog

## Unreleased

### Fixed

- Cloudflare Pages listing requests `per_page=10` and walks every page. The live `GET /accounts/{account_id}/pages/projects` API returns HTTP 400 for `per_page` 15, 20, 25, and 50.
- `cost --output json` uses the same snake_case resource schema as `rip --output json`. Unpriced rows, including Cloudflare inventory, have `cost_usd: null` instead of `0`. Priced rows from other providers stay numeric, including a real zero estimate.
- JSON monetary totals (`estimated_total`, `actual_total`, snapshot `total_usd`, and snapshot service and resource amounts) are rounded to the nearest cent.
- Cost summaries say `across 1 service` and `across N services`.
