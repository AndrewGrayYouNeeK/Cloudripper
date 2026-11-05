package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

const readmeURL = "https://github.com/AndrewGrayYouNeeK/Cloudripper#readme"

// credentialHelp tells people how to set up each provider's credentials.
var credentialHelp = map[string]string{
	"aws":   "AWS:   run `aws configure`, or set AWS_PROFILE (or AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY)",
	"gcp":   "GCP:   run `gcloud auth application-default login`, and set GCP_PROJECT",
	"azure": "Azure: run `az login`, and set AZURE_SUBSCRIPTION_ID",
}

// credentialsError is a short, readable "no credentials" failure. It carries
// no raw SDK output, so it is safe to print as is.
type credentialsError struct {
	providers []string
}

func (e *credentialsError) Error() string {
	var b strings.Builder
	if len(e.providers) == 0 {
		b.WriteString("no cloud credentials found.\n")
	} else {
		fmt.Fprintf(&b, "no credentials found for %s.\n", strings.Join(labels(e.providers), ", "))
	}
	b.WriteString("\nSet up at least one cloud, then run the command again:\n")
	for _, p := range []string{"aws", "gcp", "azure"} {
		b.WriteString("  " + credentialHelp[p] + "\n")
	}
	fmt.Fprintf(&b, "\nSee the README for details: %s", readmeURL)
	return b.String()
}

func labels(providers []string) []string {
	names := map[string]string{"aws": "AWS", "gcp": "GCP", "azure": "Azure"}
	out := make([]string, 0, len(providers))
	for _, p := range providers {
		if n, ok := names[p]; ok {
			out = append(out, n)
		} else {
			out = append(out, p)
		}
	}
	return out
}

// isCredentialProblem reports whether one scan error line is about missing
// or unusable credentials rather than a permissions or network problem.
func isCredentialProblem(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{
		"no ec2 imds role",
		"failed to refresh cached credentials",
		"nocredentialproviders",
		"get credentials",
		"could not find default credentials",
		"defaultazurecredential",
		"no valid credential",
		"credentials are not available",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// friendlyScanError turns a scan that produced nothing into a short message
// when every failure is a missing-credentials problem. Other errors keep
// their text. It returns nil for a nil error.
func friendlyScanError(err error) error {
	if err == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(err.Error(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !isCredentialProblem(line) {
			return fmt.Errorf("scan failed: %s", humanizeJoined(err))
		}
		prefix, _, ok := strings.Cut(line, ":")
		prefix = strings.ToLower(strings.TrimSpace(prefix))
		if ok && credentialHelp[prefix] != "" {
			seen[prefix] = true
		}
	}
	if len(seen) == 0 {
		return &credentialsError{}
	}
	var providers []string
	for p := range seen {
		providers = append(providers, p)
	}
	sort.Strings(providers)
	return &credentialsError{providers: providers}
}

// isCredentialsError reports whether err is the friendly credentials error.
func isCredentialsError(err error) bool {
	var ce *credentialsError
	return errors.As(err, &ce)
}
