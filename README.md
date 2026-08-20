# CloudRipper

**Multi-cloud orchestration that scales your infra, slashes your cloud bill, and intentionally breaks things on purpose.**

Built like a Soviet brutalist building — heavy, ugly, and impossible to kill.

## Features

- **Auto-scaling** across AWS and GCP
- **Real-time cost optimization** — finds expensive garbage and rips it
- **Chaos engineering** baked in (Chaos Mesh / Gremlin)
- Terraform + Pulumi living together in unholy matrimony
- Native Go CLI — no third-party backends, no vendor lock-in

## Commands

```bash
# Scan all configured cloud providers
cloudripper rip

# Analyze spend and get cost-cutting recommendations
cloudripper optimize

# Inject chaos experiments (requires CHAOS_ENDPOINT)
cloudripper chaos --target my-app --kind PodChaos --duration 30s
```

All commands support `--dry-run` to preview without making changes.

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `AWS_REGION` | AWS region to scan | `us-east-1` |
| `GCP_PROJECT` | GCP project ID | — |
| `GCP_REGION` | GCP region | `us-central1` |
| `CHAOS_ENDPOINT` | Chaos Mesh or Gremlin endpoint | — |
| `CHAOS_PROVIDER` | `chaos-mesh` or `gremlin` | `chaos-mesh` |
| `CHAOS_API_KEY` | Gremlin API key | — |
| `DRY_RUN` | Preview mode | `false` |

AWS credentials are loaded from the standard credential chain (`~/.aws/credentials`, `AWS_PROFILE`, etc.).

## Installation

```bash
go install github.com/AndrewGrayYouNeeK/cloudripper@latest
```

Or build from source:

```bash
git clone https://github.com/AndrewGrayYouNeeK/Cloudripper.git
cd Cloudripper
go build -o cloudripper .
```

## Infrastructure

### Terraform

```bash
cd infrastructure
terraform init
terraform plan -var="google_project=YOUR_GCP_PROJECT"
terraform apply
```

### Pulumi

```bash
cd pulumi
npm install
pulumi config set gcpProject YOUR_GCP_PROJECT
pulumi up
```

## Project Structure

```
.
├── cmd/                  # CLI commands (rip, chaos, optimize)
├── internal/
│   ├── cloud/            # AWS + GCP resource scanners
│   ├── chaos/            # Chaos Mesh / Gremlin orchestrator
│   ├── config/           # Environment configuration
│   └── optimizer/        # Cost analysis engine
├── infrastructure/       # Terraform modules
└── pulumi/               # Pulumi TypeScript stack
```

## Warning

This tool will save you money and possibly delete your production cluster. Use at your own risk.
