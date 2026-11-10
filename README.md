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

# 2. Look at fictional sample output. No credentials and no network.
#    The resources, costs, and savings figures are made up.
cloudripper demo

# 3. Point it at an account. AWS uses the normal SDK credential chain.
export AWS_PROFILE=my-readonly-profile
export AWS_REGION=us-east-1

# 4. Scan, then look at spend and savings ideas
cloudripper rip                 # inventory what is running
cloudripper cost                # estimated vs. actual trailing 30-day spend
cloudripper optimize            # recommendations only; nothing changes without --apply

# 5. Hunt for bills you forgot about (no cloud credentials needed)
cloudripper find-bills --sample
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
- **Output you can script.** `--output table|json|csv|html` on every command that prints rows. HTML is one self-contained file: no external assets, values are escaped, unpriced costs are `n/a`, and missing actuals are `unavailable` rather than `$0`.

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
cloudripper demo
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

`--output table` (the default), `json`, `csv`, or `html` works on every command that prints rows. `CLOUDRIPPER_OUTPUT` sets the same value when the flag is omitted. CSV cells that start with `=`, `+`, `-`, or `@` are prefixed so spreadsheets do not treat them as formulas. `html` writes one document with inline CSS and no scripts, images, or network requests. Every value is escaped. An unpriced resource is `n/a`. Actual spend that the billing APIs did not return is `unavailable`, not `$0`.

Table output uses ANSI color only when stdout is a terminal. `NO_COLOR` (any non-empty value) and `--no-color` turn it off. `json`, `csv`, and `html` are never colored.

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
| `CLOUDRIPPER_OUTPUT` | `table`, `json`, `csv`, or `html` | `table` |
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
| Pages inventory | `GET /accounts/{account_id}/pages/projects` | Pages Read (permission catalog label: Cloudflare Pages Read) |
| Invoice/billing-history actuals | `GET /accounts/{account_id}/billing/history` | Billing Read |

You can grant only the product reads you need. Missing scopes produce a warning for that product and leave other inventory intact. Billing Read is optional: without it, `cost`/`rip` still list resources and report Cloudflare actual spend as unavailable.

## Minimum read-only permissions

These lists are the read calls in this repository. They are not a live IAM simulation. `optimize --apply`, `schedule --apply`, and `scale --apply` need extra write permissions on AWS, GCP, and Azure. Cloudflare `Execute` returns an error and does not call a write API (`internal/cloud/cloudflare.go`).

### AWS

Read calls, by SDK method:

| SDK call | Where | IAM action |
| --- | --- | --- |
| `ec2:DescribeInstances` | `internal/cloud/aws_scan.go`, and `internal/scale/aws.go` for an `ec2` policy | `ec2:DescribeInstances` |
| `ec2:DescribeVolumes` | `internal/cloud/aws_scan.go` | `ec2:DescribeVolumes` |
| `ec2:DescribeAddresses` | `internal/cloud/aws_scan.go` | `ec2:DescribeAddresses` |
| `ec2:DescribeRegions` | `internal/cloud/regions.go`, only with `--all-regions` | `ec2:DescribeRegions` |
| `rds:DescribeDBInstances` | `internal/cloud/aws_scan.go` | `rds:DescribeDBInstances` |
| `s3:ListBuckets` | `internal/cloud/aws_scan.go` | `s3:ListAllMyBuckets` (the IAM action AWS documents for ListBuckets) |
| `s3:GetBucketLocation` | `internal/cloud/aws_scan.go`, once per listed bucket | `s3:GetBucketLocation` |
| `s3:GetBucketTagging` | `internal/cloud/aws_scan.go`, once per bucket kept for the scan | `s3:GetBucketTagging` |
| `elasticloadbalancing:DescribeLoadBalancers` | `internal/cloud/aws_scan.go` | `elasticloadbalancing:DescribeLoadBalancers` |
| `elasticloadbalancing:DescribeTargetGroups` | `internal/cloud/aws_scan.go` | `elasticloadbalancing:DescribeTargetGroups` |
| `elasticloadbalancing:DescribeTargetHealth` | `internal/cloud/aws_scan.go`, once per target group | `elasticloadbalancing:DescribeTargetHealth` |
| `lambda:ListFunctions` | `internal/cloud/aws_scan.go` | `lambda:ListFunctions` |
| `eks:ListClusters` | `internal/cloud/aws_scan.go` | `eks:ListClusters` |
| `eks:DescribeCluster` | `internal/cloud/aws_scan.go`, once per cluster name | `eks:DescribeCluster` |
| `autoscaling:DescribeAutoScalingGroups` | `internal/cloud/aws_scan.go`, and `internal/scale/aws.go` | `autoscaling:DescribeAutoScalingGroups` |
| `cloudwatch:GetMetricStatistics` | `internal/scale/aws.go` only (`scale`, not `rip`) | `cloudwatch:GetMetricStatistics` |
| `ce:GetCostAndUsage` | `internal/cost/aws.go` | `ce:GetCostAndUsage` |
| `ce:GetCostAndUsageWithResources` | `internal/cost/aws.go`, only when a region filter is set | `ce:GetCostAndUsageWithResources` |

`s3:ListAllMyBuckets` is the action named on the [ListBuckets API page](https://docs.aws.amazon.com/AmazonS3/latest/API/API_ListBuckets.html). The other IAM names match the SDK operation names above. This repo did not call `iam:SimulatePrincipalPolicy`.

Policy for those read actions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "CloudRipperRead",
      "Effect": "Allow",
      "Action": [
        "autoscaling:DescribeAutoScalingGroups",
        "ce:GetCostAndUsage",
        "ce:GetCostAndUsageWithResources",
        "cloudwatch:GetMetricStatistics",
        "ec2:DescribeAddresses",
        "ec2:DescribeInstances",
        "ec2:DescribeRegions",
        "ec2:DescribeVolumes",
        "eks:DescribeCluster",
        "eks:ListClusters",
        "elasticloadbalancing:DescribeLoadBalancers",
        "elasticloadbalancing:DescribeTargetGroups",
        "elasticloadbalancing:DescribeTargetHealth",
        "lambda:ListFunctions",
        "rds:DescribeDBInstances",
        "s3:GetBucketLocation",
        "s3:GetBucketTagging",
        "s3:ListAllMyBuckets"
      ],
      "Resource": "*"
    }
  ]
}
```

`cloudwatch:GetMetricStatistics` is only used by `scale`. `ec2:DescribeRegions` is only used with `--all-regions`. Drop either action if you do not use that command.

The published AWS managed policy [ReadOnlyAccess](https://docs.aws.amazon.com/aws-managed-policy/latest/reference/ReadOnlyAccess.html) version v191 (edited 2026-10-30) includes `ce:GetCostAndUsage`, `ce:GetCostAndUsageWithResources`, `ec2:Describe*`, `rds:Describe*`, `s3:Get*` and `s3:List*`, `elasticloadbalancing:Describe*`, `lambda:List*`, `eks:Describe*` and `eks:List*`, `autoscaling:Describe*`, and `cloudwatch:Get*`. That was read from the policy page while writing this section. It is not true of every historical version: the same page shows the policy has been edited many times. A permissions boundary, an SCP, or an older attached version can still omit Cost Explorer. Cost Explorer also has to be enabled in the Billing console before these actions return data. This repository did not attach the managed policy to an account.

Writes used by apply and scale, and not included above, are in `internal/cloud/aws_exec.go` and `internal/scale/aws.go`: EC2 stop/start/terminate, `CreateTags`, `ModifyInstanceAttribute`, `DeleteVolume`, `ReleaseAddress`; RDS stop/start/`ModifyDBInstance`; `DeleteLoadBalancer`; Auto Scaling `SetDesiredCapacity` and EC2 `ModifyInstanceAttribute` for scale. RDS terminate is refused in code.

### GCP

Inventory in `internal/cloud/gcp_sdk.go` calls, when `GCP_PROJECT` is set:

- `compute.instances.aggregatedList`, `disks.aggregatedList`, `addresses.aggregatedList`, `forwardingRules.aggregatedList`, `globalForwardingRules.list`, `instanceGroupManagers.aggregatedList`
- `storage.buckets.list`
- `cloudfunctions.projects.locations.functions.list`
- `container.projects.locations.clusters.list` (one `Do()` call, not paged)

Billing, when `GCP_BILLING_TABLE` is set, calls BigQuery `jobs.query` (`internal/cost/gcp_sdk.go`). `scale` calls Cloud Monitoring `timeSeries.list` (`internal/scale/gcp.go`).

OAuth scopes requested by those clients, from `google.golang.org/api` v0.209.0 (the version in `go.mod`):

| Client | Constant | Scope |
| --- | --- | --- |
| Compute, Storage, Cloud Functions, GKE | `CloudPlatformScope` | `https://www.googleapis.com/auth/cloud-platform` |
| BigQuery | `bigquery.BigqueryScope` | `https://www.googleapis.com/auth/bigquery` |
| Monitoring (`scale` only) | `monitoring.MonitoringReadScope` | `https://www.googleapis.com/auth/monitoring.read` |

`cloud-platform` and `bigquery` are broader than the methods above. The code does not request a narrower scope.

Roles checked against Google's IAM pages while writing this section:

- [Compute Viewer](https://cloud.google.com/iam/docs/roles-permissions/compute) (`roles/compute.viewer`) is documented as read-only get and list of Compute Engine resources. The code's aggregatedList and list calls are the list surface for instances, disks, addresses, forwarding rules, and instance group managers. This section did not diff every permission token in that role.
- [BigQuery Job User](https://cloud.google.com/bigquery/docs/access-control) (`roles/bigquery.jobUser`) includes `bigquery.jobs.create`, which is what `jobs.query` needs to start a job.
- BigQuery Data Viewer (`roles/bigquery.dataViewer`), granted on the billing export dataset, is documented as permission to get and query table data. The export table also has to exist. Both the job role and data access are required; the job role alone cannot read the table.

Cloud Functions list, GKE cluster list, `storage.buckets.list`, and Monitoring `timeSeries.list` are called by the code. This section did not re-check which predefined role contains each of those permission tokens, including `storage.buckets.list` for `Buckets.List`. Confirm the current role on [Google's IAM role reference](https://cloud.google.com/iam/docs/roles-overview) before you grant one.

### Azure

When `AZURE_SUBSCRIPTION_ID` is set, `internal/cloud/azure_sdk.go` lists:

- virtual machines (`NewListAllPager`) and, for each VM that passes the optional resource-group filter, `InstanceView`
- disks, public IP addresses, load balancers, storage accounts, web apps (Function apps are the sites whose kind contains `functionapp`), and AKS managed clusters

`internal/cost/azure.go` calls Cost Management `QueryClient.Usage` once per run, scope `/subscriptions/{id}`.

Microsoft's [Cost Management scopes](https://learn.microsoft.com/en-us/azure/cost-management-billing/costs/understand-work-scopes) page lists the built-in Reader role as read-only for the Query API, and Cost Management Reader as view-only access to cost data. Reader is the role that also covers ordinary resource reads; Cost Management Reader is not an inventory role. This section did not download the Reader role definition, so it does not claim that role JSON contains every action the SDK emits. The SDK calls above are what the code issues.

### Cloudflare

The token scope table in [Configuration](#cloudflare-token-scopes-read-only) is the permission list. Inventory is read-only. Billing history is a separate Billing Read call and is not required to list Workers, R2, KV, D1, or Pages. Per-resource Cloudflare costs stay unpriced.

## What will this cost me in API calls?

Counts below are what this code issues. List calls that loop on a page token make one request per page, so a small account is one request per list and a large account is more. Credential or token fetches made by the SDKs are not included. `demo` and `find-bills --sample` make none.

### Commands that scan and read spend

`rip`, `cost`, `optimize`, and `schedule` each call `loadResolution` once (`cmd/ui.go`): one inventory scan, then one pass over the configured billing sources. `watch` does that same collection on every sample (`cmd/watch.go`, `internal/watch/watch.go`). The first sample runs immediately. Later samples run on `WATCH_INTERVAL` (default 15 minutes) until the process stops. `watch --once` is a single collection.

AWS is always attempted. For one region, with every list fitting in a single page:

| Call | Requests |
| --- | --- |
| `DescribeInstances`, `DescribeVolumes`, `DescribeDBInstances`, `DescribeLoadBalancers`, `DescribeTargetGroups`, `ListFunctions`, `ListClusters`, `DescribeAutoScalingGroups` | 1 per call, plus another request for each extra page |
| `DescribeTargetHealth` | 1 per target group |
| `DescribeCluster` | 1 per cluster name returned by `ListClusters` |
| `DescribeAddresses` | 1 (not paged) |
| `ListBuckets` | 1, plus another request for each extra page |
| `GetBucketLocation` | 1 per bucket `ListBuckets` returned, including buckets outside the scanned region |
| `GetBucketTagging` | 1 per bucket kept after the region check |

`--all-regions` adds one `DescribeRegions`, repeats the non-S3 scan once per enabled region, and lists S3 once. Opt-in regions that are not enabled are skipped.

Cost Explorer (`internal/cost/aws.go`), on each `rip`, `cost`, `optimize`, `schedule`, or `watch` sample, when the client is constructed:

- `GetCostAndUsage`: 1 request. The client does not follow `NextPageToken`.
- `GetCostAndUsageWithResources`: 1 request when a region is set. 0 requests with `--all-regions`, because the region filter is cleared and the resource-level call is skipped.

If `NewAWSExplorer` fails, that sample sends no Cost Explorer request and records a warning.

GCP runs only when `GCP_PROJECT` is set. Each aggregatedList or list is one HTTP request per page. GKE `clusters.list` is one request. BigQuery is one `jobs.query` when `GCP_BILLING_TABLE` is set, and no BigQuery request when it is empty (the source fails locally with "GCP_BILLING_TABLE is not set").

Azure runs only when `AZURE_SUBSCRIPTION_ID` is set. Each list pager is at least one request, plus one `InstanceView` per virtual machine that passes the resource-group filter. Cost Management `QueryClient.Usage` is one request.

Cloudflare runs only when both `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_API_TOKEN` are set. Inventory (`internal/cloud/cloudflare.go`, `internal/cloud/cloudflare_api.go`):

| Endpoint | Requests |
| --- | --- |
| `GET /accounts/{id}/workers/scripts` | 1 (not paged) |
| `GET /accounts/{id}/r2/buckets` | at least 1 for each of `default`, `eu`, and `us`; more when a cursor is returned (`per_page` 100) |
| `GET /accounts/{id}/storage/kv/namespaces` | at least 1 (`per_page` 100) |
| `GET /accounts/{id}/d1/database` | at least 1 (`per_page` 100) |
| `GET /accounts/{id}/pages/projects` | at least 1 (`per_page` 20) |

Billing history is a separate paged `GET /accounts/{id}/billing/history` (`per_page` 50), one or more requests, and only on the cost pass.

### Other commands

| Command | Cloud billing and inventory APIs |
| --- | --- |
| `scale` | No Cost Explorer, BigQuery, Azure Cost Management, or Cloudflare billing. AWS: paged `DescribeAutoScalingGroups`, `DescribeInstances` for an `ec2` policy, and one `GetMetricStatistics` per group. GCP: paged instance-group list and one Monitoring `timeSeries.list` per group. |
| `find-bills` | No cloud inventory or cost APIs. IMAP or Gmail only when those sources are configured. `--csv` is a local file. `--sample` is the embedded file and does not use the network. |
| `chaos` | No cloud billing APIs. With `CHAOS_ENDPOINT` unset it prints a plan and does not POST. With an endpoint it POSTs to that URL. |
| `demo` | No network. |

Slack and email run only when `watch` raises an alert and `SLACK_WEBHOOK_URL` or the SMTP variables are set. They are not called on every sample.

### Cost Explorer price

AWS's [Cost Explorer pricing page](https://aws.amazon.com/aws-cost-management/aws-cost-explorer/pricing/) says each API request against the primary billing view costs $0.01, and the [Cost Explorer FAQ](https://aws.amazon.com/aws-cost-management/aws-cost-explorer/faqs/) says a paginated result bills each page as a separate request. Checked against those pages while writing this section. This CLI sends one `GetCostAndUsage` and, unless `--all-regions` skips it, one `GetCostAndUsageWithResources`, and it does not page those responses. That is two Cost Explorer requests per `rip`, `cost`, `optimize`, or `schedule` run in the default single-region case, and one request with `--all-regions`. `watch` repeats that count on every sample. The same pricing page says a request that uses a custom billing view costs $0.01 per source view. This CLI does not set a billing view.

Prices for the other AWS calls, and for GCP, Azure, and Cloudflare, are not stated here. Check each provider's current pricing page.

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
internal/cloud/       AWS, GCP, Azure, and Cloudflare inventory and mutations
internal/demo/        Embedded fictional inventory, spend, and card export
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
