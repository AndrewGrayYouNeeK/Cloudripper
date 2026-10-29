package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds runtime settings for Cloudripper.
type Config struct {
	AWSRegion          string
	AllRegions         bool
	GCPProject         string
	GCPRegion          string
	GCPBillingTable    string
	AzureSubscription  string
	AzureResourceGroup string
	ChaosEndpoint      string
	ChaosProvider      string
	ChaosAPIKey        string
	DryRun             bool
	AuditLog           string
	SlackWebhook       string
	SMTPHost           string
	SMTPPort           int
	SMTPUser           string
	SMTPPassword       string
	AlertEmailFrom     string
	AlertEmailTo       string
	WatchInterval      time.Duration
	WatchThreshold     float64
	ChaosMaxDuration   time.Duration
	ChaosMaxTargets    int
}

// Load reads configuration from environment variables.
func Load() Config {
	return Config{
		AWSRegion:          envOr("AWS_REGION", "us-east-1"),
		GCPProject:         os.Getenv("GCP_PROJECT"),
		GCPRegion:          envOr("GCP_REGION", "us-central1"),
		GCPBillingTable:    os.Getenv("GCP_BILLING_TABLE"),
		AzureSubscription:  os.Getenv("AZURE_SUBSCRIPTION_ID"),
		AzureResourceGroup: os.Getenv("AZURE_RESOURCE_GROUP"),
		ChaosEndpoint:      os.Getenv("CHAOS_ENDPOINT"),
		ChaosProvider:      envOr("CHAOS_PROVIDER", "chaos-mesh"),
		ChaosAPIKey:        os.Getenv("CHAOS_API_KEY"),
		DryRun:             os.Getenv("DRY_RUN") == "true",
		AuditLog:           envOr("CLOUDRIPPER_AUDIT_LOG", "cloudripper-audit.log"),
		SlackWebhook:       os.Getenv("SLACK_WEBHOOK_URL"),
		SMTPHost:           os.Getenv("SMTP_HOST"),
		SMTPPort:           envInt("SMTP_PORT", 587),
		SMTPUser:           os.Getenv("SMTP_USER"),
		SMTPPassword:       os.Getenv("SMTP_PASSWORD"),
		AlertEmailFrom:     os.Getenv("ALERT_EMAIL_FROM"),
		AlertEmailTo:       os.Getenv("ALERT_EMAIL_TO"),
		WatchInterval:      envDuration("WATCH_INTERVAL", 15*time.Minute),
		WatchThreshold:     envFloat("WATCH_THRESHOLD", 0.25),
		ChaosMaxDuration:   envDuration("CHAOS_MAX_DURATION", 5*time.Minute),
		ChaosMaxTargets:    envInt("CHAOS_MAX_TARGETS", 20),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
