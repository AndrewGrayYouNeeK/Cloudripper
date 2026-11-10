package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const cloudflareAPIBase = "https://api.cloudflare.com/client/v4"

// CloudflareAPI talks to the Cloudflare REST API with a scoped API token.
// It never logs the token. Callers should pass secrets only via constructors.
type CloudflareAPI struct {
	AccountID string
	Token     string
	BaseURL   string
	HTTP      *http.Client
}

func (c *CloudflareAPI) base() string {
	if c != nil && c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return cloudflareAPIBase
}

func (c *CloudflareAPI) client() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

type cfEnvelope struct {
	Success    bool            `json:"success"`
	Errors     []cfAPIError    `json:"errors"`
	Messages   json.RawMessage `json:"messages"`
	Result     json.RawMessage `json:"result"`
	ResultInfo *cfResultInfo   `json:"result_info"`
}

type cfAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfResultInfo struct {
	Page       int    `json:"page"`
	PerPage    int    `json:"per_page"`
	Count      int    `json:"count"`
	TotalCount int    `json:"total_count"`
	TotalPages int    `json:"total_pages"`
	Cursor     string `json:"cursor"`
}

func (c *CloudflareAPI) doJSON(ctx context.Context, method, path string, query url.Values, headers map[string]string, out *cfEnvelope) error {
	if c == nil {
		return fmt.Errorf("cloudflare client not configured")
	}
	if strings.TrimSpace(c.AccountID) == "" {
		return fmt.Errorf("cloudflare account id not configured (set CLOUDFLARE_ACCOUNT_ID)")
	}
	if strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("cloudflare api token not configured (set CLOUDFLARE_API_TOKEN)")
	}
	u := c.base() + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return redactCloudflareErr(err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return redactCloudflareErr(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return redactCloudflareErr(err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("cloudflare %s %s: decode response (HTTP %d): %w", method, path, resp.StatusCode, redactCloudflareErr(err))
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &CloudflareAPIError{
			Status:  resp.StatusCode,
			Path:    path,
			Message: firstCFError(out.Errors, http.StatusText(resp.StatusCode)),
		}
	}
	if resp.StatusCode >= 400 || !out.Success {
		return &CloudflareAPIError{
			Status:  resp.StatusCode,
			Path:    path,
			Message: firstCFError(out.Errors, fmt.Sprintf("HTTP %d", resp.StatusCode)),
		}
	}
	return nil
}

// CloudflareAPIError is a Cloudflare REST failure without secrets.
type CloudflareAPIError struct {
	Status  int
	Path    string
	Message string
}

func (e *CloudflareAPIError) Error() string {
	if e == nil {
		return "cloudflare api error"
	}
	msg := strings.TrimSpace(e.Message)
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden {
		return fmt.Sprintf("%s: permission denied (%s)", e.Path, msg)
	}
	return fmt.Sprintf("%s: %s", e.Path, msg)
}

func firstCFError(errs []cfAPIError, fallback string) string {
	for _, e := range errs {
		if strings.TrimSpace(e.Message) != "" {
			return e.Message
		}
	}
	return fallback
}

// redactCloudflareErr strips bearer tokens from error text.
func redactCloudflareErr(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	redacted := redactCloudflareSecret(msg)
	if redacted == msg {
		return err
	}
	return fmt.Errorf("%s", redacted)
}

func redactCloudflareSecret(s string) string {
	const marker = "Bearer "
	var b strings.Builder
	for {
		i := strings.Index(s, marker)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i+len(marker)])
		b.WriteString("[redacted]")
		rest := s[i+len(marker):]
		end := strings.IndexAny(rest, " \t\r\n\"'")
		if end < 0 {
			return b.String()
		}
		s = rest[end:]
	}
}

func (c *CloudflareAPI) getPaged(ctx context.Context, path string, query url.Values, headers map[string]string, perPage int, handle func(result json.RawMessage, info *cfResultInfo) error) error {
	if query == nil {
		query = url.Values{}
	}
	if perPage <= 0 {
		perPage = 100
	}
	query.Set("per_page", strconv.Itoa(perPage))
	page := 1
	cursor := query.Get("cursor")
	for i := 0; i < 1000; i++ {
		usedCursor := cursor != ""
		q := cloneValues(query)
		if usedCursor {
			q.Set("cursor", cursor)
			q.Del("page")
		} else {
			q.Set("page", strconv.Itoa(page))
			q.Del("cursor")
		}
		var env cfEnvelope
		if err := c.doJSON(ctx, http.MethodGet, path, q, headers, &env); err != nil {
			return err
		}
		if err := handle(env.Result, env.ResultInfo); err != nil {
			return err
		}
		if env.ResultInfo == nil {
			return nil
		}
		// Cursor-style pagination (R2 buckets). An empty cursor ends the sequence.
		if env.ResultInfo.Cursor != "" {
			cursor = env.ResultInfo.Cursor
			continue
		}
		if usedCursor {
			return nil
		}
		total := env.ResultInfo.TotalCount
		per := env.ResultInfo.PerPage
		if per <= 0 {
			per = perPage
		}
		current := page
		if env.ResultInfo.Page > 0 {
			current = env.ResultInfo.Page
		}
		// Pages (and some other list endpoints) report total_pages. Honor it
		// before total_count so a short page is not treated as the last page.
		if env.ResultInfo.TotalPages > 0 {
			if current >= env.ResultInfo.TotalPages {
				return nil
			}
			page = current + 1
			cursor = ""
			continue
		}
		if total > 0 && current*per >= total {
			return nil
		}
		// No total_count and no cursor: stop when this page looks short.
		if total == 0 && countRawItems(env.Result) < per {
			return nil
		}
		if total == 0 && env.ResultInfo.Count > 0 && env.ResultInfo.Count < per {
			return nil
		}
		page = current + 1
		cursor = ""
	}
	return fmt.Errorf("%s: pagination exceeded safety limit", path)
}

func cloneValues(in url.Values) url.Values {
	out := make(url.Values, len(in))
	for k, vs := range in {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func countRawItems(raw json.RawMessage) int {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return 0
		}
		return len(items)
	}
	return 1
}

// CloudflareWorkerScript is one Workers script listing.
type CloudflareWorkerScript struct {
	ID         string   `json:"id"`
	CreatedOn  string   `json:"created_on"`
	ModifiedOn string   `json:"modified_on"`
	Handlers   []string `json:"handlers"`
	UsageModel string   `json:"usage_model"`
}

// CloudflareR2Bucket is one R2 bucket.
type CloudflareR2Bucket struct {
	Name         string `json:"name"`
	CreationDate string `json:"creation_date"`
	Location     string `json:"location"`
	Jurisdiction string `json:"jurisdiction"`
	StorageClass string `json:"storage_class"`
}

type cloudflareR2ListResult struct {
	Buckets []CloudflareR2Bucket `json:"buckets"`
}

// CloudflareKVNamespace is one Workers KV namespace.
type CloudflareKVNamespace struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Jurisdiction string `json:"jurisdiction"`
}

// CloudflareD1Database is one D1 database.
type CloudflareD1Database struct {
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	CreatedAt    string `json:"created_at"`
	Version      string `json:"version"`
	Jurisdiction string `json:"jurisdiction"`
}

// CloudflarePagesProject is one Pages project.
type CloudflarePagesProject struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Subdomain        string `json:"subdomain"`
	CreatedOn        string `json:"created_on"`
	ProductionBranch string `json:"production_branch"`
}

// ListWorkersScripts lists uploaded Worker scripts for the account.
// Endpoint: GET /accounts/{account_id}/workers/scripts
// Scope: Workers Scripts Read (docs: Accepted Permissions on that endpoint).
func (c *CloudflareAPI) ListWorkersScripts(ctx context.Context) ([]CloudflareWorkerScript, error) {
	path := "/accounts/" + url.PathEscape(c.AccountID) + "/workers/scripts"
	var env cfEnvelope
	if err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &env); err != nil {
		return nil, err
	}
	var scripts []CloudflareWorkerScript
	if len(env.Result) > 0 && string(env.Result) != "null" {
		if err := json.Unmarshal(env.Result, &scripts); err != nil {
			return nil, fmt.Errorf("workers/scripts: decode result: %w", err)
		}
	}
	return scripts, nil
}

// ListR2Buckets lists R2 buckets in one jurisdiction (cursor pagination).
// Endpoint: GET /accounts/{account_id}/r2/buckets
// Scope: Workers R2 Storage Read.
func (c *CloudflareAPI) ListR2Buckets(ctx context.Context, jurisdiction string) ([]CloudflareR2Bucket, error) {
	path := "/accounts/" + url.PathEscape(c.AccountID) + "/r2/buckets"
	headers := map[string]string{}
	if jurisdiction != "" && jurisdiction != "default" {
		headers["cf-r2-jurisdiction"] = jurisdiction
	}
	var all []CloudflareR2Bucket
	query := url.Values{}
	err := c.getPaged(ctx, path, query, headers, 100, func(result json.RawMessage, _ *cfResultInfo) error {
		if len(result) == 0 || string(result) == "null" {
			return nil
		}
		var wrapped cloudflareR2ListResult
		if err := json.Unmarshal(result, &wrapped); err != nil {
			return fmt.Errorf("r2/buckets: decode result: %w", err)
		}
		for i := range wrapped.Buckets {
			if wrapped.Buckets[i].Jurisdiction == "" {
				wrapped.Buckets[i].Jurisdiction = jurisdiction
				if wrapped.Buckets[i].Jurisdiction == "" {
					wrapped.Buckets[i].Jurisdiction = "default"
				}
			}
		}
		all = append(all, wrapped.Buckets...)
		return nil
	})
	return all, err
}

// ListKVNamespaces lists Workers KV namespaces (page pagination).
// Endpoint: GET /accounts/{account_id}/storage/kv/namespaces
// Scope: Workers KV Storage Read.
func (c *CloudflareAPI) ListKVNamespaces(ctx context.Context) ([]CloudflareKVNamespace, error) {
	path := "/accounts/" + url.PathEscape(c.AccountID) + "/storage/kv/namespaces"
	var all []CloudflareKVNamespace
	err := c.getPaged(ctx, path, nil, nil, 100, func(result json.RawMessage, _ *cfResultInfo) error {
		var page []CloudflareKVNamespace
		if len(result) == 0 || string(result) == "null" {
			return nil
		}
		if err := json.Unmarshal(result, &page); err != nil {
			return fmt.Errorf("kv/namespaces: decode result: %w", err)
		}
		all = append(all, page...)
		return nil
	})
	return all, err
}

// ListD1Databases lists D1 databases (page pagination).
// Endpoint: GET /accounts/{account_id}/d1/database
// Scope: D1 Read.
func (c *CloudflareAPI) ListD1Databases(ctx context.Context) ([]CloudflareD1Database, error) {
	path := "/accounts/" + url.PathEscape(c.AccountID) + "/d1/database"
	var all []CloudflareD1Database
	err := c.getPaged(ctx, path, nil, nil, 100, func(result json.RawMessage, _ *cfResultInfo) error {
		var page []CloudflareD1Database
		if len(result) == 0 || string(result) == "null" {
			return nil
		}
		if err := json.Unmarshal(result, &page); err != nil {
			return fmt.Errorf("d1/database: decode result: %w", err)
		}
		all = append(all, page...)
		return nil
	})
	return all, err
}

// pagesListPerPage is the largest page size the live Pages list endpoint accepts.
//
// Docs mark per_page as optional and the sample result_info uses 20:
// https://developers.cloudflare.com/api/resources/pages/subresources/projects/methods/list/
// Omitting the parameter would keep that server default. A live read-only check
// of GET /accounts/{account_id}/pages/projects returns HTTP 400
// "Invalid list options provided. Review the page or per_page parameter"
// for per_page 15, 20, 25, and 50, and HTTP 200 for 1, 5, and 10.
const pagesListPerPage = 10

// ListPagesProjects lists Cloudflare Pages projects (page pagination).
// Endpoint: GET /accounts/{account_id}/pages/projects
// Scope: Pages Read (API docs) / Cloudflare Pages Read (permission catalog).
func (c *CloudflareAPI) ListPagesProjects(ctx context.Context) ([]CloudflarePagesProject, error) {
	path := "/accounts/" + url.PathEscape(c.AccountID) + "/pages/projects"
	var all []CloudflarePagesProject
	err := c.getPaged(ctx, path, nil, nil, pagesListPerPage, func(result json.RawMessage, _ *cfResultInfo) error {
		var page []CloudflarePagesProject
		if len(result) == 0 || string(result) == "null" {
			return nil
		}
		if err := json.Unmarshal(result, &page); err != nil {
			return fmt.Errorf("pages/projects: decode result: %w", err)
		}
		all = append(all, page...)
		return nil
	})
	return all, err
}

// CloudflareBillingItem is one account billing history row.
type CloudflareBillingItem struct {
	ID          string  `json:"id"`
	Action      string  `json:"action"`
	Amount      float64 `json:"amount"`
	AmountToPay float64 `json:"amount_to_pay"`
	Currency    string  `json:"currency"`
	Description string  `json:"description"`
	OccurredAt  string  `json:"occurred_at"`
	Status      string  `json:"status"`
	Type        string  `json:"type"`
}

// ListBillingHistory lists account billing history (page pagination).
// Endpoint: GET /accounts/{account_id}/billing/history
// Scope: Billing Read.
// This is invoice/billing-history data, not resource-level usage metering.
func (c *CloudflareAPI) ListBillingHistory(ctx context.Context) ([]CloudflareBillingItem, error) {
	path := "/accounts/" + url.PathEscape(c.AccountID) + "/billing/history"
	var all []CloudflareBillingItem
	err := c.getPaged(ctx, path, nil, nil, 50, func(result json.RawMessage, _ *cfResultInfo) error {
		var page []CloudflareBillingItem
		if len(result) == 0 || string(result) == "null" {
			return nil
		}
		if err := json.Unmarshal(result, &page); err != nil {
			return fmt.Errorf("billing/history: decode result: %w", err)
		}
		all = append(all, page...)
		return nil
	})
	return all, err
}

// ParseCloudflareTime parses Cloudflare API timestamps.
func ParseCloudflareTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
	}
	var last error
	for _, layout := range formats {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t.UTC(), nil
		}
		last = err
	}
	return time.Time{}, last
}
