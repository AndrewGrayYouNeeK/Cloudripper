package config

import (
	"os"
)

// Config holds runtime settings for Cloudripper.
type Config struct {
	AWSRegion      string
	GCPProject     string
	GCPRegion      string
	ChaosEndpoint  string
	ChaosProvider  string
	ChaosAPIKey    string
	DryRun         bool
}

// Load reads configuration from environment variables.
func Load() Config {
	return Config{
		AWSRegion:     envOr("AWS_REGION", "us-east-1"),
		GCPProject:    os.Getenv("GCP_PROJECT"),
		GCPRegion:     envOr("GCP_REGION", "us-central1"),
		ChaosEndpoint: os.Getenv("CHAOS_ENDPOINT"),
		ChaosProvider: envOr("CHAOS_PROVIDER", "chaos-mesh"),
		ChaosAPIKey:   os.Getenv("CHAOS_API_KEY"),
		DryRun:        os.Getenv("DRY_RUN") == "true",
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
