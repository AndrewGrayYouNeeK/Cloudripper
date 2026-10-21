# CloudRipper

CloudRipper is a Go CLI for AWS, GCP, and Azure. It inventories resources, reads actual trailing spend when billing APIs are available, applies cost actions you confirm, scales instance groups from CPU, watches for cost spikes, and posts chaos experiments inside a blast-radius limit.

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
```

`--output table` (the default), `json`, or `csv` works on every command that prints rows. `CLOUDRIPPER_OUTPUT` sets the same value when the flag is omitted. CSV cells that start with `=`, `+`, `-`, or `@` are prefixed so spreadsheets do not treat them as formulas.

`--dry-run` previews mutations. `optimize`, `schedule`, and `scale` also require `--apply` before they will change anything, and `--dry-run` still wins if both are set. Live changes ask for one confirmation on a terminal, or accept `--yes` when stdin is not a terminal.

## What a scan covers

| Provider | Inventoried | Mutations |
| --- | --- | --- |
| AWS | EC2, EBS, unattached Elastic IPs, RDS, S3, ALB/NLB, Lambda, EKS, Auto Scaling groups | Stop, start, terminate, or resize EC2. Stop, start, or resize RDS. Delete an available unattached volume, an unattached address, or an idle load balancer. Tag an EC2 schedule. |
| GCP | GCE, disks, addresses, GCS, forwarding rules, Cloud Functions, GKE, managed instance groups | Stop, start, delete, or resize a GCE instance. Delete an unattached disk, an unused address, or an idle forwarding rule. Label a GCE schedule. |
| Azure | Virtual machines, managed disks, public IPs, storage accounts, load balancers, Function apps, AKS | Deallocate, start, delete, or resize a VM. Delete an unattached disk, an unused public IP, or a load balancer with empty backend pools. Tag a VM schedule. |

S3, GCS, Lambda, Cloud Functions, Azure Functions, storage accounts, EKS, GKE, and AKS are inventory only. CloudRipper does not delete databases or Kubernetes clusters. RDS terminate returns an error; stop and rightsize are the supported database actions. Stopped EC2 compute is priced at $0 when EBS volumes are listed on their own, so disk cost is not counted twice. Lambda, object storage, and Kubernetes control-plane lines stay at a $0 estimate until a billing export matches them. EKS and GKE clusters use a flat control-plane estimate. Auto Scaling groups and managed instance groups are listed at $0 because their instances are counted separately.

A provider that fails mid-scan does not drop resources the other providers already returned. The warning is printed and the command continues.

## Cost data

`rip` and `cost` call:

- **AWS Cost Explorer** for daily `UnblendedCost` in the scan region over the trailing 30 days. The API client uses `us-east-1`. Service totals are kept when the resource-level call fails; that call is often disabled on an account, and the failure is a warning.
- **GCP Cloud Billing BigQuery export** when `GCP_PROJECT` and `GCP_BILLING_TABLE` (`project.dataset.table`) are set. The query nets credits. Any other table string is rejected.
- **Azure Cost Management** for the subscription, optionally limited to one resource group.

Resource rows are replaced with actuals only when a resource-level billing row matches the resource ID or name. Otherwise the row keeps its estimate and `cost_source` stays `estimate`.

The built-in prices are heuristics (a small EC2/GCE/Azure size table, plus flat monthly amounts for unattached IPs, load balancers, and Kubernetes control planes). They are not a price list.

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

Flags override the matching variables: `--aws-region`, `--gcp-project`, `--gcp-region`, `--azure-subscription`, `--azure-resource-group`, `--audit-log`, `--output`, `--dry-run`, `--yes`.

AWS uses the standard SDK credential chain. GCP uses application default credentials. Azure uses `DefaultAzureCredential`. Cost Explorer, Compute, Monitoring, BigQuery, and Azure Cost Management each need their own read permission. Apply and scale need the matching write permissions.

## Install

```bash
go install github.com/AndrewGrayYouNeeK/cloudripper@latest
```

From source:

```bash
git clone https://github.com/AndrewGrayYouNeeK/Cloudripper.git
cd Cloudripper
go build -o cloudripper .
```

Container image:

```bash
docker build -t cloudripper .
docker run --rm cloudripper rip --help
```

Release archives are defined in `.goreleaser.yaml` (linux, darwin, and windows; amd64 and arm64; CGO disabled). Tag a version and run `goreleaser release`, or `goreleaser release --snapshot --clean` locally.

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
internal/iac/         Terraform state and Pulumi export detection
internal/notify/      Slack, email, stdout
infrastructure/       Sample Terraform stack
pulumi/               Sample Pulumi program
```
