<p align="center">
  <img src="docs/logo.png" alt="YouNeeK CloudRipper: the N is a purple lightning bolt ripping through storm clouds" width="480">
</p>

<h1 align="center">YouNeeK CloudRipper</h1>

<p align="center"><strong>One open-source CLI for cloud waste, real spend, and safe chaos tests, across AWS, GCP, Azure, and Cloudflare.</strong></p>

<p align="center">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/License-MIT-yellow.svg"></a>
  <a href="https://github.com/AndrewGrayYouNeeK/Cloudripper/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/AndrewGrayYouNeeK/Cloudripper/actions/workflows/ci.yml/badge.svg"></a>
  <a href="go.mod"><img alt="Go 1.22+" src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://cloudripper.youneek.dev"><img alt="Website" src="https://img.shields.io/badge/site-cloudripper.youneek.dev-8b5cf6"></a>
</p>

<p align="center">
  <img src="docs/demo.gif" alt="Terminal demo: cloudripper --help, then find-bills reading a sample card export" width="820">
</p>

<sub>The demo needs no cloud credentials: <code>--help</code>, then <code>find-bills</code> on the sample card export in <a href="docs/examples/sample-card-export.csv"><code>docs/examples/</code></a>. The sample charges are made up.</sub>

Find what you are paying for and not using, catch the cloud and developer subscriptions you forgot about, and test how your systems fail. Everything runs on your machine with your own credentials. There is no hosted backend.

## Quickstart

```bash
# 1. Install: grab a prebuilt binary from the Releases page (no Go needed),
#    or build it yourself with Go 1.22+:
go install github.com/AndrewGrayYouNeeK/Cloudripper/cmd/cloudripper@latest

# 2. Point it at an account. AWS uses the normal SDK credential chain.
export AWS_PROFILE=my-readonly-profile
export AWS_REGION=us-east-1

# 3. Scan, then look at spend and savings ideas
cloudripper rip                 # inventory what is running
cloudripper cost                # estimated vs. actual trailing 30-day spend
cloudripper optimize            # recommendations only; nothing changes without --apply

# 4. Hunt for bills you forgot about (no cloud credentials needed)
cloudripper find-bills --csv card-export.csv
```

`cost` and `rip` use AWS Cost Explorer for actual spend. Enable it first in the Billing console; data can take about 24 hours to appear. For GCP, set `GCP_PROJECT`; for Azure, set `AZURE_SUBSCRIPTION_ID`; for Cloudflare, set `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_API_TOKEN`. See [Configuration](#configuration).

## Features

- **Waste scan.** `rip` inventories compute, disks, addresses, load balancers, databases, storage, functions, and Kubernetes across AWS, GCP, and Azure, plus Cloudflare Workers, R2, KV, D1, and Pages.
- **Real spend.** `cost` reads trailing 30-day spend from Cost Explorer, BigQuery billing export, Azure Cost Management, or Cloudflare billing history when those APIs are available, and reports estimates and actuals separately.
- **Savings recommendations.** `optimize` suggests deleting unattached resources, rightsizing, and scheduling. Changes need `--apply` plus a confirmation, honor `--dry-run`, and are written to an audit log.
- **Forgotten bills.** `find-bills` reads billing mail (IMAP or Gmail, read-only) and a card CSV for AWS, Google Cloud, Azure, DigitalOcean, Linode/Akamai, Vercel, Netlify, Heroku, Cloudflare, Supabase, Firebase, MongoDB Atlas, and OpenAI, and flags the ones you have not set up.
- **Scheduling, scaling, and cost alerts.** `schedule`, `scale`, and `watch` stop idle compute on a schedule, scale groups from CPU, and alert Slack or email on spend spikes.
- **Bounded chaos tests.** `chaos` runs Chaos Mesh or Gremlin experiments inside a blast-radius limit, and refuses production-looking targets without `--allow-prod`.
- **Output you can script.** `--output table|json|csv` on every command that prints rows.

## Free and paid

The CLI is free and MIT licensed.

| | Price | Status |
| --- | --- | --- |
| **CLI** | Free | Available now (this repo) |
| **Pro** | $29/month | Coming soon. [Join the waitlist](https://cloudripper.youneek.dev/#waitlist) |
| **Team / Business** | From $199/month | [Get in touch](mailto:andrewgray@usedartifacts.com) |

More at [cloudripper.youneek.dev](https://cloudripper.youneek.dev).

## How it works

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

One CLI for cloud waste, real spend, and safe chaos tests, across AWS, GCP, Azure, and Cloudflare.

It inventories resources, reads actual trailing spend when billing APIs are available, applies cost actions you confirm, scales instance groups from CPU, watches for cost spikes, and posts chaos experiments inside a blast-radius limit.

Pricing heuristics fill in when a billing API is missing or denies the call. Estimates and actuals are reported separately because they measure different things: an estimate is a monthly price guess for one resource, and an actual is account spend over the last 30 days.

## Commands

```bash
cloudripper rip
cloudripper cost
cloudripper optimize
cloudripper optimize --apply
cloudripper scale
cloudripper scale --policy policy.json --apply
cloudripper watch --once
cloudripper schedule
cloudripper chaos --target my-app --kind PodChaos --duration 30s
cloudripper rip --all-regions
cloudripper find-bills --csv card-export.csv
```

`--output table` (the default), `json`, or `csv` works on every command that prints rows. `CLOUDRIPPER_OUTPUT` sets the same value when the flag is omitted. CSV cells that start with `=`, `+`, `-`, or `@` are prefixed so spreadsheets do not treat them as formulas.

`--dry-run` previews mutations. `optimize`, `schedule`, and `scale` also require `--apply` before they will change anything, and `--dry-run` still wins if both are set. Live changes ask for one confirmation on a terminal, or accept `--yes` when stdin is not a terminal.

## What a scan covers

| Provider | Inventoried | Mutations |
| --- | --- | --- |
| AWS | EC2, EBS, unattached Elastic IPs, RDS, S3, ALB/NLB, Lambda, EKS, Auto Scaling groups | Stop, start, terminate, or resize EC2. Stop, start, or resize RDS. Delete an available unattached volume, an unattached address, or an idle load balancer. Tag an EC2 schedule. |
| GCP | GCE, disks, addresses, GCS, forwarding rules, Cloud Functions, GKE, managed instance groups | Stop, start, delete, or resize a GCE instance. Delete an unattached disk, an unused address, or an idle forwarding rule. Label a GCE schedule. |
| Azure | Virtual machines, managed disks, public IPs, storage accounts, load balancers, Function apps, AKS | Deallocate, start, delete, or resize a VM. Delete an unattached disk, an unused public IP, or a load balancer with empty backend pools. Tag a VM schedule. |
| Cloudflare | Workers scripts, R2 buckets (`default`/`eu`/`us` jurisdictions), KV namespaces, D1 databases, Pages projects | None. The Cloudflare integration is read-only and refuses `optimize`/`schedule`/`scale` mutations. Zones and other Cloudflare products are not inventoried yet. |

`--all-regions` asks EC2 `DescribeRegions` for every region and scans the ones that are enabled. Opt-in regions that are not enabled are skipped. With that flag, Cost Explorer is not limited to a single region. `scale` still uses `--aws-region`.

S3 buckets are global, so each scan calls `GetBucketLocation` and keeps only buckets in the regions being scanned. An empty location constraint is `us-east-1`, and the legacy `EU` constraint is `eu-west-1`. A bucket in another region does not appear with a blank region column.

When `GCP_PROJECT`, `AZURE_SUBSCRIPTION_ID`, or Cloudflare credentials are unset, scan commands print a `skipped:` line for that cloud. A missing credential error is rewritten the same way. Other scan failures stay warnings and do not drop resources that were already found. For Cloudflare, a missing product permission (for example R2 Read without KV Read) is reported for that product and does not discard Workers/D1/Pages that succeeded.

S3, GCS, Lambda, Cloud Functions, Azure Functions, storage accounts, EKS, GKE, and AKS are inventory only. CloudRipper does not delete databases or Kubernetes clusters. RDS terminate returns an error; stop and rightsize are the supported database actions. Stopped EC2 compute is priced at $0 when EBS volumes are listed on their own, so disk cost is not counted twice. Lambda, object storage, and Kubernetes control-plane lines stay at a $0 estimate until a billing export matches them. EKS and GKE clusters use a flat control-plane estimate. Auto Scaling groups and managed instance groups are listed at $0 because their instances are counted separately.

Cloudflare inventory rows use `cost_source=unpriced` with `cost_usd: null` / table `n/a`. That means per-resource cost is unavailable, not a calculated $0 estimate. `estimated_total` excludes unpriced rows and reports `unpriced_count` so mixed AWS/GCP/Azure + Cloudflare totals do not pretend coverage is complete. Cloudflare is not VM-centric: CloudRipper does not invent stop/start, rightsizing, schedule tags, or age-based “unused” recommendations for Workers, R2, KV, D1, or Pages.

A provider that fails mid-scan does not drop resources the other providers already returned. The warning is printed and the command continues.

## Cost data

`rip` and `cost` call:

- **AWS Cost Explorer** for daily `UnblendedCost` in the scan region over the trailing 30 days (`--all-regions` drops the region filter). The API client uses `us-east-1`. Service totals are kept when the resource-level call fails. If the account is not enabled for Cost Explorer, the message says to enable it in the Billing console and that data usually appears after about 24 hours. `rip` and `cost` print that warning once on stderr for table and CSV output, and once inside JSON output.
- **GCP Cloud Billing BigQuery export** when `GCP_PROJECT` and `GCP_BILLING_TABLE` (`project.dataset.table`) are set. The query nets credits. Any other table string is rejected.
- **Azure Cost Management** for the subscription, optionally limited to one resource group.
- **Cloudflare account billing history** (`GET /accounts/{account_id}/billing/history`) when `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_API_TOKEN` are set and the token has **Billing Read** (dashboard: Account → Billing → Read). Official API docs accept API token Bearer auth for this endpoint. CloudRipper pages the list (the endpoint has no date-range query), keeps items whose `occurred_at` falls in the trailing 30-day window, and sums only rows with currency `USD`. Missing or non-USD currencies are skipped with warnings. The snapshot source is `billing-history`: account invoice/charge events, **not** Workers Analytics, GraphQL usage, or per-resource metering. Without Billing Read or on auth failure, Cloudflare actual spend stays unavailable (never a fabricated $0 invoice total). Inventory rows remain `unpriced` because billing-history does not attribute charges to Workers/R2/KV/D1/Pages IDs.

Resource rows are replaced with actuals only when a resource-level billing row matches the resource ID or name. Otherwise priced rows keep their estimate (`cost_source=estimate`). Unpriced rows stay unavailable.

`rip --output json` and `cost --output json` use the same document: `dry_run`, `estimated_total`, `actual_total`, `actual_available`, `unpriced_count`, `warnings`, `snapshots`, and `resources`. Resource objects are snake_case (`id`, `name`, `cost_usd`, `cost_source`, and the other inventory fields). Unpriced rows, including every Cloudflare inventory row, set `cost_usd` to `null`. Table and CSV show those rows as `n/a`. A priced AWS, GCP, or Azure row stays a number, including `0` when the estimate really is zero. `cost --output json` previously emitted exported Go names (`ID`, `CostUSD`, and the rest) and wrote `0` for unpriced rows. See [CHANGELOG](CHANGELOG.md).

JSON money is rounded to the nearest cent: `estimated_total`, `actual_total`, each snapshot `total_usd`, and the snapshot service and resource amounts. A Cloudflare billing snapshot of $6.12 summed with a tiny Cost Explorer residual prints as `6.12`, not `6.1199999999`.

The built-in prices are heuristics (a small EC2/GCE/Azure size table, plus flat monthly amounts for unattached IPs, load balancers, and Kubernetes control planes). They are not a price list. Cloudflare inventory does not invent list-price estimates.

## Optimize and schedule

`optimize` recommends, in order of fit:

- terminate an unattached disk, an unused public IP, or an idle load balancer
- rightsize a database or a compute instance whose estimated cost is at least $100
- terminate a stopped compute instance that still shows a cost of at least $1
- tag other running compute with `cloudripper:schedule=off=utc:19-07`

Rightsizing steps one rung on a fixed ladder (`t3.nano` … `c5.4xlarge`, `e2-micro` … `n1-standard-4`, `Standard_B1s` … `Standard_D8s_v3`). The proposed size is shown before it is applied. EC2 resize stops a running instance, changes the type, and starts it again only if it was running. If the type change fails, a previously running instance is started again. An instance that was already stopped stays stopped.

`cloudripper:protect=true` (also `yes`, `y`, or `1`) removes a resource from recommendations and from apply. Protected resources are never passed to a cloud API by `optimize` or `schedule`.

`--apply` performs the actions after confirmation. Each result is appended to the audit log as JSONL (`CLOUDRIPPER_AUDIT_LOG`, default `cloudripper-audit.log`). Terminating an instance, volume, address, or load balancer is irreversible.

`schedule` is pull-based. It reads `cloudripper:schedule` tags of the form `off=utc:19-07` or `off=utc:19:00-07:30` and stops running compute inside the window or starts stopped compute outside it. The window is UTC, start inclusive, end exclusive, and may cross midnight. CloudRipper does not create EventBridge rules or Cloud Scheduler jobs. Run `schedule --apply` from cron, or `watch --enforce-schedules`.

## Auto-scaling

`scale` reads CPU from CloudWatch (`CPUUtilization`, 15 minutes, period 5 minutes) and from Cloud Monitoring for GCP. The default metric thresholds are 70% scale-out and 30% scale-in, step 1, cooldown 5 minutes.

With no policy file, the command discovers Auto Scaling groups and managed instance groups only. It does not resize every EC2 or GCE instance. A policy file can target `asg`, `mig`, `ec2`, or `gce`:

```json
{
  "policies": [
    {
      "name": "web",
      "provider": "aws",
      "kind": "asg",
      "target": "web",
      "min": 1,
      "max": 6,
      "scaleOutThreshold": 70,
      "scaleInThreshold": 30,
      "step": 1,
      "cooldown": "5m"
    }
  ]
}
```

`ec2` and `gce` policies change the machine type one ladder step. Azure scale is not implemented. Pass `--state path.json` to keep cooldown across runs. A discovered group with no CPU data is reported as failed and the rest of the pass continues.

## Watch

`watch` samples immediately, then on `WATCH_INTERVAL` (default 15m). The first sample is a baseline and does not alert. A later sample alerts when total spend, or one cloud's spend, rises by `WATCH_THRESHOLD` versus the previous sample. `0.25` and `25` both mean 25 percent. A zero baseline does not alert. A sustained high value alerts once, because the next comparison uses the previous sample.

Slack uses `SLACK_WEBHOOK_URL`. Email uses `SMTP_HOST`, `SMTP_PORT` (587), `SMTP_USER`, `SMTP_PASSWORD`, `ALERT_EMAIL_FROM`, and `ALERT_EMAIL_TO`. With neither sink configured, the alert is printed. `--state` stores samples so a restart still has a baseline. `--enforce-schedules` runs due schedule actions; a live run requires `--yes`.

## Forgotten bills

`find-bills` looks through mail and an optional card CSV for invoices from AWS, Google Cloud, Azure, DigitalOcean, Linode/Akamai, Vercel, Netlify, Heroku, Cloudflare, Supabase, Firebase, MongoDB Atlas, and OpenAI. For each provider it reports the account id when one is present, the latest amount and date, and whether CloudRipper is already configured for that provider. Anything else is marked `possibly forgotten`.

```bash
export CLOUDRIPPER_IMAP_HOST=imap.example.com
export CLOUDRIPPER_IMAP_USER=you@example.com
export CLOUDRIPPER_IMAP_PASSWORD=app-password
cloudripper find-bills

cloudripper find-bills --gmail-token "$HOME/.cloudripper/gmail-token.json"
cloudripper find-bills --csv card-export.csv --since 2400h
```

IMAP logs in, then `EXAMINE`s the mailbox and fetches `BODY.PEEK`. It does not mark mail read, delete it, or change flags. Gmail is a series of GET requests. The token file is JSON with `access_token` (and optional `expiry`) and needs the `gmail.readonly` scope. The command does not print passwords, tokens, or message bodies. Card amounts are shown as positive charges. Rows that do not match the provider list are ignored.

AWS is reported as configured because the CLI always scans it. GCP is configured when `GCP_PROJECT` is set, Azure when `AZURE_SUBSCRIPTION_ID` is set, and Cloudflare when both `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_API_TOKEN` are set.

## Chaos

`chaos` posts to Chaos Mesh (`POST /api/v1/experiments`) or Gremlin (`POST /v1/attacks`). The action is `pod-kill`, `delay`, or `cpu` for `PodChaos`, `NetworkChaos`, and `StressChaos` unless `--action` overrides it.

Safety checks reject an empty, `*`, or `all` target, a duration above `CHAOS_MAX_DURATION` (default 5m), more than `CHAOS_MAX_TARGETS` targets (default 20), and the `kube-system`, `kube-public`, and `kube-node-lease` namespaces. Names containing `prod`, `production`, or `prd` need `--allow-prod`.

`--wait` polls status. `--rollback` (default true) aborts a failed experiment. `chaos abort`, `chaos rollback`, and `chaos status` take `--name`. Rollback deletes the Chaos Mesh experiment or halts the Gremlin attack. It does not undo an EC2 resize or any other cloud mutation.

Without `CHAOS_ENDPOINT`, `chaos` prints the default plan and does not post it.

## Terraform and Pulumi

Resources tagged `ManagedBy=terraform` or `ManagedBy=pulumi`, `terraform=true`, or any `pulumi:*` key are treated as infrastructure-as-code. `--iac-state` can point at a Terraform state file (v4 `resources[].instances[].attributes.id` / `arn`), a Pulumi export (`deployment.resources`, skipping `pulumi:` types and non-custom resources), or a directory of `*.tfstate` and `*pulumi*.json` files.

`optimize --apply` and `schedule --apply` write a snippet under `.cloudripper/plans` (or `--plan-dir`) and skip the live call. `--force-iac` performs the call anyway and will drift state. CloudRipper does not edit `.tf` or TypeScript programs.

The `infrastructure/` Terraform stack and `pulumi/` program in this repo are sample stacks. They are separate from the CLI's state detection.

## Configuration

| Variable | Description | Default |
| --- | --- | --- |
| `AWS_REGION` | AWS region to scan | `us-east-1` |
| `GCP_PROJECT` | GCP project ID | — |
| `GCP_REGION` | GCP region filter | `us-central1` |
| `GCP_BILLING_TABLE` | Billing export `project.dataset.table` | — |
| `AZURE_SUBSCRIPTION_ID` | Azure subscription | — |
| `AZURE_RESOURCE_GROUP` | Optional Azure resource group filter | — |
| `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account ID | — |
| `CLOUDFLARE_API_TOKEN` | Cloudflare API token (not Global API Key). Keep out of logs and commits. | — |
| `CLOUDRIPPER_AUDIT_LOG` | JSONL audit log | `cloudripper-audit.log` |
| `CLOUDRIPPER_OUTPUT` | `table`, `json`, or `csv` | `table` |
| `SLACK_WEBHOOK_URL` | Slack incoming webhook | — |
| `SMTP_HOST` / `SMTP_PORT` | Alert email server | port `587` |
| `SMTP_USER` / `SMTP_PASSWORD` | SMTP auth, skipped when user is empty | — |
| `ALERT_EMAIL_FROM` / `ALERT_EMAIL_TO` | Alert envelope | — |
| `WATCH_INTERVAL` | Watch period | `15m` |
| `WATCH_THRESHOLD` | Spike ratio or percent | `0.25` |
| `CHAOS_ENDPOINT` | Chaos Mesh or Gremlin base URL | — |
| `CHAOS_PROVIDER` | `chaos-mesh` or `gremlin` | `chaos-mesh` |
| `CHAOS_API_KEY` | Bearer token (required for Gremlin) | — |
| `CHAOS_MAX_DURATION` | Blast-radius duration | `5m` |
| `CHAOS_MAX_TARGETS` | Blast-radius target count | `20` |
| `DRY_RUN` | Preview mutations | `false` |
| `CLOUDRIPPER_IMAP_HOST` | IMAP server for `find-bills` | — |
| `CLOUDRIPPER_IMAP_PORT` | IMAP TLS port | `993` |
| `CLOUDRIPPER_IMAP_USER` | IMAP user | — |
| `CLOUDRIPPER_IMAP_PASSWORD` | IMAP app password (not logged) | — |
| `CLOUDRIPPER_IMAP_MAILBOX` | Mailbox to examine | `INBOX` |
| `CLOUDRIPPER_GMAIL_TOKEN` | Gmail OAuth token JSON path | — |

Flags override the matching variables: `--aws-region`, `--gcp-project`, `--gcp-region`, `--azure-subscription`, `--azure-resource-group`, `--cloudflare-account`, `--audit-log`, `--output`, `--dry-run`, `--yes`. The Cloudflare API token is env-only (`CLOUDFLARE_API_TOKEN`) so it is not passed on the command line.

AWS uses the standard SDK credential chain. GCP uses application default credentials. Azure uses `DefaultAzureCredential`. Cloudflare uses a customer-supplied API token plus account ID. Cost Explorer, Compute, Monitoring, BigQuery, Azure Cost Management, and Cloudflare Billing Read each need their own read permission. Apply and scale need the matching write permissions on AWS/GCP/Azure; Cloudflare mutations are not implemented.

### Cloudflare token scopes (read-only)

Create a token in the Cloudflare dashboard (API Tokens), not a Global API Key. Minimum scopes used by this integration, taken from the current endpoint “Accepted Permissions” docs:

| Feature | Endpoint | Minimum permission |
| --- | --- | --- |
| Workers inventory | `GET /accounts/{account_id}/workers/scripts` | Workers Scripts Read |
| R2 inventory | `GET /accounts/{account_id}/r2/buckets` | Workers R2 Storage Read |
| KV inventory | `GET /accounts/{account_id}/storage/kv/namespaces` | Workers KV Storage Read |
| D1 inventory | `GET /accounts/{account_id}/d1/database` | D1 Read |
| Pages inventory | `GET /accounts/{account_id}/pages/projects` | Pages Read (permission catalog label: Cloudflare Pages Read). The client sends `per_page=10` and follows `page` until `total_pages` is exhausted. The API docs mark `per_page` as optional and show a sample of 20, but the live endpoint returns HTTP 400 for `per_page` above 10. |
| Invoice/billing-history actuals | `GET /accounts/{account_id}/billing/history` | Billing Read |

You can grant only the product reads you need. Missing scopes produce a warning for that product and leave other inventory intact. Billing Read is optional: without it, `cost`/`rip` still list resources and report Cloudflare actual spend as unavailable.

## Install

Prebuilt binaries: download the archive for your system from the [latest release](https://github.com/AndrewGrayYouNeeK/Cloudripper/releases/latest) (Linux, macOS, and Windows; amd64 and arm64). Check it against `checksums.txt`, unpack it, and put `cloudripper` on your `PATH`. For example, on Linux amd64:

```bash
VERSION=0.1.0
curl -LO https://github.com/AndrewGrayYouNeeK/Cloudripper/releases/download/v${VERSION}/cloudripper_${VERSION}_linux_amd64.tar.gz
tar xzf cloudripper_${VERSION}_linux_amd64.tar.gz
sudo mv cloudripper /usr/local/bin/
cloudripper --help
```

The binaries are not code-signed, so macOS may ask you to allow it in System Settings, and Windows SmartScreen may warn.

Or with Go 1.22 or newer:

```bash
go install github.com/AndrewGrayYouNeeK/Cloudripper/cmd/cloudripper@latest
```

This puts a `cloudripper` binary in `$(go env GOPATH)/bin`. Add that directory to your `PATH` if it is not already there.

From source:

```bash
git clone https://github.com/AndrewGrayYouNeeK/Cloudripper.git
cd Cloudripper
go build -o cloudripper ./cmd/cloudripper
```

Container image:

```bash
docker build -t cloudripper .
docker run --rm cloudripper rip --help
```

Release archives are built with [GoReleaser](https://goreleaser.com) from `.goreleaser.yaml` (linux, darwin, and windows; amd64 and arm64; CGO disabled). To cut one, tag a version and run `goreleaser release`, or try `goreleaser release --snapshot --clean` locally.

## Tests

```bash
go test -race -cover ./...
go vet ./...
```

Unit tests use interfaces and fakes. They do not call AWS, GCP, or Azure.

## Limits

- No live cloud integration tests in CI.
- Rightsizing ladders are heuristics, not compatibility checks.
- GCP managed-instance-group CPU uses the `instance_group` metric label and is best-effort for the group's zone or region.
- Schedule enforcement is pull-based.
- Azure has inventory and VM actions, and no scale command.
- Database deletion and Kubernetes cluster deletion are refused.
- Chaos rollback aborts the experiment only.
- EC2 resize restarts an instance that was running if the type change fails. Other cloud mutations are not rolled back.

## Layout

```
cmd/                  CLI commands
internal/cloud/       AWS, GCP, and Azure inventory and mutations
internal/cost/        Cost Explorer, BigQuery billing, Azure Cost Management
internal/optimizer/   Recommendations
internal/apply/       Confirmation, protect tags, IaC guard, audit
internal/scale/       ASG, MIG, EC2, and GCE policies
internal/watch/       Interval sampling and spike detection
internal/chaos/       Chaos Mesh / Gremlin plus blast-radius checks
internal/bills/       Read-only billing mail and card CSV matching
internal/iac/         Terraform state and Pulumi export detection
internal/notify/      Slack, email, stdout
infrastructure/       Sample Terraform stack
pulumi/               Sample Pulumi program
```

## License

[MIT](LICENSE). Copyright 2026 Andrew Gray.
