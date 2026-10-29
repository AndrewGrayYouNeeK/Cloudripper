package bills

import "regexp"

// Provider is one cloud or developer platform we look for in mail and card exports.
type Provider struct {
	ID       string
	Name     string
	Domains  []string
	Keywords []string
	Account  *regexp.Regexp
}

// Catalog is the provider list find-bills matches. More specific entries come
// first so Firebase is not reported as Google Cloud.
func Catalog() []Provider {
	return []Provider{
		{
			ID:       "aws",
			Name:     "AWS",
			Domains:  []string{"amazonaws.com", "aws.amazon.com"},
			Keywords: []string{"amazon web services", "aws.amazon", "amzn aws"},
			Account:  regexp.MustCompile(`(?i)account(?:\s+id)?[:\s#]+(\d{4}-\d{4}-\d{4}|\d{12})\b`),
		},
		{
			ID:       "azure",
			Name:     "Microsoft Azure",
			Domains:  []string{"azure.com", "azure.microsoft.com"},
			Keywords: []string{"microsoft azure", "azure"},
			Account:  regexp.MustCompile(`(?i)subscription(?:\s+id)?[:\s]+([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`),
		},
		{
			ID:       "firebase",
			Name:     "Firebase",
			Domains:  []string{"firebase.com", "firebase.google.com"},
			Keywords: []string{"firebase"},
		},
		{
			ID:       "gcp",
			Name:     "Google Cloud",
			Domains:  []string{"cloud.google.com", "googlecloud.com"},
			Keywords: []string{"google cloud", "google cloud platform"},
			Account:  regexp.MustCompile(`(?i)project(?:\s+id)?[:\s]+([a-z][a-z0-9-]{4,28}[a-z0-9])`),
		},
		{
			ID:       "digitalocean",
			Name:     "DigitalOcean",
			Domains:  []string{"digitalocean.com"},
			Keywords: []string{"digitalocean"},
		},
		{
			ID:       "linode",
			Name:     "Linode/Akamai",
			Domains:  []string{"linode.com", "akamai.com"},
			Keywords: []string{"linode", "akamai"},
		},
		{
			ID:       "vercel",
			Name:     "Vercel",
			Domains:  []string{"vercel.com"},
			Keywords: []string{"vercel"},
		},
		{
			ID:       "netlify",
			Name:     "Netlify",
			Domains:  []string{"netlify.com"},
			Keywords: []string{"netlify"},
		},
		{
			ID:       "heroku",
			Name:     "Heroku",
			Domains:  []string{"heroku.com"},
			Keywords: []string{"heroku"},
		},
		{
			ID:       "cloudflare",
			Name:     "Cloudflare",
			Domains:  []string{"cloudflare.com"},
			Keywords: []string{"cloudflare"},
		},
		{
			ID:       "supabase",
			Name:     "Supabase",
			Domains:  []string{"supabase.com", "supabase.io"},
			Keywords: []string{"supabase"},
		},
		{
			ID:       "mongodb",
			Name:     "MongoDB Atlas",
			Domains:  []string{"mongodb.com"},
			Keywords: []string{"mongodb atlas", "mongodb"},
		},
		{
			ID:       "openai",
			Name:     "OpenAI",
			Domains:  []string{"openai.com"},
			Keywords: []string{"openai"},
		},
	}
}

// Accounts says which catalog providers CloudRipper is already set up to scan.
// AWS is always scanned. GCP and Azure count only when a project or subscription is set.
// Every other provider is possibly forgotten.
type Accounts struct {
	AWS   bool
	GCP   bool
	Azure bool
}

// Has reports whether CloudRipper is configured for the provider id.
func (a Accounts) Has(id string) bool {
	switch id {
	case "aws":
		return a.AWS
	case "gcp":
		return a.GCP
	case "azure":
		return a.Azure
	default:
		return false
	}
}
