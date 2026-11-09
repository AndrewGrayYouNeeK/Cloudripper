package cloud

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Cloudflare developer-platform resource types. These are not VMs; stop/start
// rightsizing and age-based idle heuristics do not apply.
const (
	ResourceCFWorker ResourceType = "cf-worker"
	ResourceCFR2     ResourceType = "cf-r2"
	ResourceCFKV     ResourceType = "cf-kv"
	ResourceCFD1     ResourceType = "cf-d1"
	ResourceCFPages  ResourceType = "cf-pages"
)

// CostSourceCloudflareBilling labels invoice/billing-history charges from
// GET /accounts/{account_id}/billing/history. It is account-level invoice data
// filtered by occurred_at, not resource-level usage metering.
const CostSourceCloudflareBilling = "billing-history"

// CloudflareLister lists account inventory. Tests fake this; CloudflareAPI implements it.
type CloudflareLister interface {
	ListWorkersScripts(ctx context.Context) ([]CloudflareWorkerScript, error)
	ListR2Buckets(ctx context.Context, jurisdiction string) ([]CloudflareR2Bucket, error)
	ListKVNamespaces(ctx context.Context) ([]CloudflareKVNamespace, error)
	ListD1Databases(ctx context.Context) ([]CloudflareD1Database, error)
	ListPagesProjects(ctx context.Context) ([]CloudflarePagesProject, error)
}

// CloudflareInventory is the normalized Cloudflare scan result.
type CloudflareInventory struct {
	Workers []CloudflareWorkerScript
	R2      []CloudflareR2Bucket
	KV      []CloudflareKVNamespace
	D1      []CloudflareD1Database
	Pages   []CloudflarePagesProject
}

// CloudflareProvider scans one Cloudflare account. Mutations are refused.
type CloudflareProvider struct {
	accountID string
	token     string
	lister    CloudflareLister
	connect   func() (CloudflareLister, error)

	loaded  bool
	loadErr error
}

// NewCloudflareProvider creates a provider that uses CLOUDFLARE_API_TOKEN auth.
func NewCloudflareProvider(accountID, token string) *CloudflareProvider {
	accountID = strings.TrimSpace(accountID)
	token = strings.TrimSpace(token)
	p := &CloudflareProvider{accountID: accountID, token: token}
	p.connect = func() (CloudflareLister, error) {
		if accountID == "" {
			return nil, fmt.Errorf("cloudflare account id not configured (set CLOUDFLARE_ACCOUNT_ID)")
		}
		if token == "" {
			return nil, fmt.Errorf("cloudflare api token not configured (set CLOUDFLARE_API_TOKEN)")
		}
		return &CloudflareAPI{AccountID: accountID, Token: token}, nil
	}
	return p
}

// NewCloudflareProviderWith wires a fake lister for tests.
func NewCloudflareProviderWith(accountID string, lister CloudflareLister) *CloudflareProvider {
	return &CloudflareProvider{accountID: accountID, lister: lister, loaded: true}
}

// Name implements Provider.
func (c *CloudflareProvider) Name() string { return "cloudflare" }

// Provider implements Executor.
func (c *CloudflareProvider) Provider() string { return "cloudflare" }

// Scan implements Provider. Partial product failures are returned alongside
// resources discovered from products that succeeded.
func (c *CloudflareProvider) Scan(ctx context.Context) ([]Resource, error) {
	if err := c.ensure(); err != nil {
		return nil, err
	}
	inv, err := InventoryCloudflare(ctx, c.lister)
	return ResourcesFromCloudflare(inv), err
}

// Execute implements Executor. Cloudflare integration is read-only.
func (c *CloudflareProvider) Execute(_ context.Context, r Resource, action, _ string) (string, error) {
	if IsProtected(r.Tags) {
		return "", fmt.Errorf("refusing to change protected resource %s", r.ID)
	}
	return "", &UnsupportedError{
		Action: action,
		Type:   r.Type,
		Reason: fmt.Sprintf("cloudflare integration is read-only; refusing to %s %s", action, r.Type),
	}
}

func (c *CloudflareProvider) ensure() error {
	if c.loaded {
		return c.loadErr
	}
	c.loaded = true
	if c.connect == nil {
		c.loadErr = fmt.Errorf("cloudflare client not configured")
		return c.loadErr
	}
	lister, err := c.connect()
	if err != nil {
		c.loadErr = err
		return err
	}
	c.lister = lister
	return nil
}

// InventoryCloudflare gathers Workers, R2, KV, D1, and Pages. Each product is
// independent: a permission failure on one does not discard the others.
func InventoryCloudflare(ctx context.Context, lister CloudflareLister) (CloudflareInventory, error) {
	var inv CloudflareInventory
	var errs []error
	if lister == nil {
		return inv, fmt.Errorf("cloudflare lister not configured")
	}

	scripts, err := lister.ListWorkersScripts(ctx)
	inv.Workers = scripts
	if err != nil {
		errs = append(errs, fmt.Errorf("workers: %w", err))
	}

	for _, jurisdiction := range []string{"default", "eu", "us"} {
		buckets, err := lister.ListR2Buckets(ctx, jurisdiction)
		inv.R2 = append(inv.R2, buckets...)
		if err != nil {
			var apiErr *CloudflareAPIError
			if errors.As(err, &apiErr) && (apiErr.Status == 401 || apiErr.Status == 403) && jurisdiction != "default" {
				errs = append(errs, fmt.Errorf("r2(%s): %w", jurisdiction, err))
				continue
			}
			errs = append(errs, fmt.Errorf("r2(%s): %w", jurisdiction, err))
		}
	}

	ns, err := lister.ListKVNamespaces(ctx)
	inv.KV = ns
	if err != nil {
		errs = append(errs, fmt.Errorf("kv: %w", err))
	}

	dbs, err := lister.ListD1Databases(ctx)
	inv.D1 = dbs
	if err != nil {
		errs = append(errs, fmt.Errorf("d1: %w", err))
	}

	pages, err := lister.ListPagesProjects(ctx)
	inv.Pages = pages
	if err != nil {
		errs = append(errs, fmt.Errorf("pages: %w", err))
	}

	return inv, errors.Join(errs...)
}

// ResourcesFromCloudflare converts inventory into Resource rows.
// Per-resource cost is CostSourceUnpriced (not a $0 estimate): Cloudflare
// products are usage-based and inventory alone does not yield a reliable
// monthly price. Account invoice charges may appear separately under
// CostSourceCloudflareBilling when Billing Read works; they are not mapped
// onto individual Workers/R2/KV/D1/Pages rows.
func ResourcesFromCloudflare(inv CloudflareInventory) []Resource {
	var resources []Resource
	for _, script := range inv.Workers {
		id := strings.TrimSpace(script.ID)
		if id == "" {
			continue
		}
		status := "active"
		spec := strings.TrimSpace(script.UsageModel)
		resources = append(resources, Resource{
			ID:         id,
			Name:       id,
			Provider:   "cloudflare",
			Type:       ResourceCFWorker,
			Region:     "account",
			Status:     status,
			Spec:       spec,
			CostUSD:    0,
			CostSource: CostSourceUnpriced,
			Metadata: map[string]string{
				"created_on":  script.CreatedOn,
				"modified_on": script.ModifiedOn,
			},
		})
	}
	seenR2 := map[string]bool{}
	for _, bucket := range inv.R2 {
		name := strings.TrimSpace(bucket.Name)
		if name == "" || seenR2[name] {
			continue
		}
		seenR2[name] = true
		jurisdiction := firstNonEmpty(bucket.Jurisdiction, "default")
		region := firstNonEmpty(bucket.Location, jurisdiction)
		resources = append(resources, Resource{
			ID:         name,
			Name:       name,
			Provider:   "cloudflare",
			Type:       ResourceCFR2,
			Region:     region,
			Status:     "listed",
			Spec:       firstNonEmpty(bucket.StorageClass, "Standard"),
			CostUSD:    0,
			CostSource: CostSourceUnpriced,
			Metadata: map[string]string{
				"jurisdiction":  jurisdiction,
				"creation_date": bucket.CreationDate,
			},
		})
	}
	for _, ns := range inv.KV {
		id := strings.TrimSpace(ns.ID)
		if id == "" {
			continue
		}
		resources = append(resources, Resource{
			ID:         id,
			Name:       firstNonEmpty(ns.Title, id),
			Provider:   "cloudflare",
			Type:       ResourceCFKV,
			Region:     firstNonEmpty(ns.Jurisdiction, "account"),
			Status:     "listed",
			CostUSD:    0,
			CostSource: CostSourceUnpriced,
			Metadata: map[string]string{
				"jurisdiction": ns.Jurisdiction,
			},
		})
	}
	for _, db := range inv.D1 {
		id := strings.TrimSpace(db.UUID)
		if id == "" {
			continue
		}
		resources = append(resources, Resource{
			ID:         id,
			Name:       firstNonEmpty(db.Name, id),
			Provider:   "cloudflare",
			Type:       ResourceCFD1,
			Region:     firstNonEmpty(db.Jurisdiction, "account"),
			Status:     firstNonEmpty(db.Version, "listed"),
			Spec:       db.Version,
			CostUSD:    0,
			CostSource: CostSourceUnpriced,
			Metadata: map[string]string{
				"created_at":   db.CreatedAt,
				"jurisdiction": db.Jurisdiction,
			},
		})
	}
	for _, project := range inv.Pages {
		id := firstNonEmpty(strings.TrimSpace(project.ID), strings.TrimSpace(project.Name))
		if id == "" {
			continue
		}
		resources = append(resources, Resource{
			ID:         id,
			Name:       firstNonEmpty(project.Name, id),
			Provider:   "cloudflare",
			Type:       ResourceCFPages,
			Region:     "account",
			Status:     "listed",
			Spec:       project.ProductionBranch,
			CostUSD:    0,
			CostSource: CostSourceUnpriced,
			Metadata: map[string]string{
				"subdomain":  project.Subdomain,
				"created_on": project.CreatedOn,
			},
		})
	}
	return resources
}

// CloudflareConfigured reports whether both account id and API token are set.
func CloudflareConfigured(accountID, token string) bool {
	return strings.TrimSpace(accountID) != "" && strings.TrimSpace(token) != ""
}
