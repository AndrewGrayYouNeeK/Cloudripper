package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/config"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"golang.org/x/oauth2/google"
)

const (
	// StatusFound means a local credential chain or the required environment
	// variables produced credentials. Secret values are never included.
	StatusFound = "found"
	// StatusMissing means the local chain or environment has nothing to use.
	StatusMissing = "missing"
	// StatusError means a credential check failed for a reason other than
	// absence. The detail is redacted.
	StatusError = "error"
)

// ProviderStatus is safe to show in the desktop app.
// Identity holds a project, subscription, account id, profile, or region.
// It never holds an access key, token, or password.
type ProviderStatus struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Identity string `json:"identity,omitempty"`
}

// ProviderStatuses checks AWS, GCP, Azure, and Cloudflare using the same
// local credential chains and environment variables as the CLI.
// Each check has a short timeout. Secret values are not returned.
func ProviderStatuses(ctx context.Context, cfg config.Config) []ProviderStatus {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	checks := []func(context.Context) ProviderStatus{
		func(ctx context.Context) ProviderStatus { return awsStatus(ctx, cfg) },
		func(ctx context.Context) ProviderStatus { return gcpStatus(ctx, cfg) },
		func(ctx context.Context) ProviderStatus { return azureStatus(ctx, cfg) },
		func(context.Context) ProviderStatus { return cloudflareStatus(cfg) },
	}
	out := make([]ProviderStatus, len(checks))
	var wg sync.WaitGroup
	for i, check := range checks {
		wg.Add(1)
		go func(i int, check func(context.Context) ProviderStatus) {
			defer wg.Done()
			out[i] = check(ctx)
		}(i, check)
	}
	wg.Wait()
	return out
}

func awsStatus(ctx context.Context, cfg config.Config) ProviderStatus {
	status := ProviderStatus{ID: "aws", Name: "AWS"}
	region := strings.TrimSpace(cfg.AWSRegion)
	if region == "" {
		region = "us-east-1"
	}
	profile := strings.TrimSpace(os.Getenv("AWS_PROFILE"))
	identity := "region " + region
	if profile != "" {
		identity = "profile " + profile + ", " + identity
	}
	status.Identity = identity

	loadCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	awsCfg, err := awsconfig.LoadDefaultConfig(loadCtx, awsconfig.WithRegion(region))
	if err != nil {
		return finishStatus(status, err, "AWS credentials were not found in the local credential chain.")
	}
	// Retrieve confirms the chain can produce credentials. The returned
	// access key and session token are discarded and never copied onto status.
	if _, err = awsCfg.Credentials.Retrieve(loadCtx); err != nil {
		return finishStatus(status, err, "AWS credentials were not found in the local credential chain.")
	}
	status.Status = StatusFound
	status.Detail = "AWS credentials are available from the local credential chain. Secret values are not shown."
	return status
}

func gcpStatus(ctx context.Context, cfg config.Config) ProviderStatus {
	status := ProviderStatus{ID: "gcp", Name: "GCP"}
	project := strings.TrimSpace(cfg.GCPProject)
	if project == "" {
		status.Status = StatusMissing
		status.Detail = "GCP project is not set (set GCP_PROJECT)."
		return status
	}
	status.Identity = "project " + project
	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// Finding credentials reads the local ADC files or metadata server.
	// The token source is not invoked, so no access token is materialized.
	_, err := google.FindDefaultCredentials(checkCtx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return finishStatus(status, err, "GCP application default credentials were not found.")
	}
	status.Status = StatusFound
	status.Detail = "GCP project and application default credentials are available. Secret values are not shown."
	return status
}

func azureStatus(ctx context.Context, cfg config.Config) ProviderStatus {
	status := ProviderStatus{ID: "azure", Name: "Azure"}
	subscription := strings.TrimSpace(cfg.AzureSubscription)
	if subscription == "" {
		status.Status = StatusMissing
		status.Detail = "Azure subscription is not set (set AZURE_SUBSCRIPTION_ID)."
		return status
	}
	status.Identity = "subscription " + subscription
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return finishStatus(status, err, "Azure credentials were not found in the local credential chain.")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// The access token is discarded immediately.
	_, err = cred.GetToken(checkCtx, policy.TokenRequestOptions{
		Scopes: []string{"https://management.azure.com/.default"},
	})
	if err != nil {
		return finishStatus(status, err, "Azure credentials were not found in the local credential chain.")
	}
	status.Status = StatusFound
	status.Detail = "Azure subscription and local credentials are available. Secret values are not shown."
	return status
}

func cloudflareStatus(cfg config.Config) ProviderStatus {
	status := ProviderStatus{ID: "cloudflare", Name: "Cloudflare"}
	account := strings.TrimSpace(cfg.CloudflareAccountID)
	token := strings.TrimSpace(cfg.CloudflareAPIToken)
	switch {
	case account == "" && token == "":
		status.Status = StatusMissing
		status.Detail = "Cloudflare is not set (set CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN)."
	case account == "":
		status.Status = StatusMissing
		status.Detail = "Cloudflare account id is not set (set CLOUDFLARE_ACCOUNT_ID). The API token is not shown."
	case token == "":
		status.Status = StatusMissing
		status.Identity = "account " + account
		status.Detail = "Cloudflare API token is not set (set CLOUDFLARE_API_TOKEN)."
	default:
		status.Status = StatusFound
		status.Identity = "account " + account
		status.Detail = "Cloudflare account id and API token are set in the environment. The token is not shown."
	}
	return status
}

func finishStatus(status ProviderStatus, err error, missingDetail string) ProviderStatus {
	if err == nil {
		status.Status = StatusFound
		return status
	}
	kind := classifyCredentialError(err)
	status.Status = kind
	switch kind {
	case StatusMissing:
		status.Detail = missingDetail
	default:
		status.Status = StatusError
		status.Detail = "Credential check failed: " + redactSecrets(err.Error())
	}
	return status
}

func classifyCredentialError(err error) string {
	if err == nil {
		return StatusFound
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "nocredentialproviders"),
		strings.Contains(lower, "no ec2 imds role"),
		strings.Contains(lower, "failed to refresh cached credentials"),
		strings.Contains(lower, "could not find default credentials"),
		strings.Contains(lower, "defaultazurecredential"),
		strings.Contains(lower, "no valid credential"),
		strings.Contains(lower, "credentials are not available"),
		strings.Contains(lower, "credentialunavailable"):
		return StatusMissing
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return StatusError
	}
	return StatusError
}

var accessKeyPattern = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)

// redactSecrets removes known environment secrets and AWS access-key ids
// from a message that might be shown in the UI.
func redactSecrets(msg string) string {
	for _, key := range []string{
		"AWS_SECRET_ACCESS_KEY",
		"AWS_ACCESS_KEY_ID",
		"AWS_SESSION_TOKEN",
		"CLOUDFLARE_API_TOKEN",
		"SMTP_PASSWORD",
		"SLACK_WEBHOOK_URL",
		"CLOUDRIPPER_IMAP_PASSWORD",
		"CLOUDRIPPER_GMAIL_TOKEN",
	} {
		secret := os.Getenv(key)
		if len(secret) < 4 {
			continue
		}
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
	}
	msg = accessKeyPattern.ReplaceAllString(msg, "[redacted]")
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 240 {
		msg = msg[:240] + "..."
	}
	return msg
}

// WatchChannels reports whether Slack and email alerts are configured.
// The webhook URL and SMTP password are not returned.
func WatchChannels(cfg config.Config) (slack bool, email bool) {
	slack = strings.TrimSpace(cfg.SlackWebhook) != ""
	email = strings.TrimSpace(cfg.SMTPHost) != "" &&
		strings.TrimSpace(cfg.AlertEmailFrom) != "" &&
		strings.TrimSpace(cfg.AlertEmailTo) != ""
	return slack, email
}

// FormatMoney formats a rounded dollar amount for display.
func FormatMoney(v float64) string {
	return fmt.Sprintf("$%.2f", v)
}
