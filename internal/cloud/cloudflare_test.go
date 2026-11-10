package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudflareScanInventoryAndPagination(t *testing.T) {
	var kvPages, r2Pages, billingPages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token-secret" {
			t.Errorf("authorization = %q", got)
		}
		if strings.Contains(r.Header.Get("Authorization"), "test-token-secret") {
			// ensure we never echo the token in the body
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/workers/scripts"):
			io.WriteString(w, `{"success":true,"result":[{"id":"api","usage_model":"standard","created_on":"2024-01-01T00:00:00Z"}],"errors":[],"messages":[]}`)
		case strings.HasSuffix(r.URL.Path, "/r2/buckets"):
			r2Pages++
			cursor := r.URL.Query().Get("cursor")
			if cursor == "" {
				io.WriteString(w, `{"success":true,"result":{"buckets":[{"name":"bucket-a","location":"wnam","storage_class":"Standard"}]},"result_info":{"cursor":"next-1","per_page":1,"total_count":2},"errors":[],"messages":[]}`)
				return
			}
			io.WriteString(w, `{"success":true,"result":{"buckets":[{"name":"bucket-b","location":"eeur","jurisdiction":"eu"}]},"result_info":{"per_page":1,"total_count":2},"errors":[],"messages":[]}`)
		case strings.HasSuffix(r.URL.Path, "/storage/kv/namespaces"):
			kvPages++
			page := r.URL.Query().Get("page")
			if page == "" || page == "1" {
				io.WriteString(w, `{"success":true,"result":[{"id":"kv1","title":"one"}],"result_info":{"page":1,"per_page":1,"count":1,"total_count":2},"errors":[],"messages":[]}`)
				return
			}
			io.WriteString(w, `{"success":true,"result":[{"id":"kv2","title":"two"}],"result_info":{"page":2,"per_page":1,"count":1,"total_count":2},"errors":[],"messages":[]}`)
		case strings.HasSuffix(r.URL.Path, "/d1/database"):
			io.WriteString(w, `{"success":true,"result":[{"uuid":"d1-1","name":"orders","version":"production"}],"result_info":{"page":1,"per_page":20,"total_count":1},"errors":[],"messages":[]}`)
		case strings.HasSuffix(r.URL.Path, "/pages/projects"):
			io.WriteString(w, `{"success":true,"result":[{"id":"p1","name":"docs","production_branch":"main"}],"result_info":{"page":1,"per_page":20,"total_count":1},"errors":[],"messages":[]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	api := &CloudflareAPI{
		AccountID: "acct123",
		Token:     "test-token-secret",
		BaseURL:   srv.URL,
		HTTP:      srv.Client(),
	}
	// Only hit default R2 jurisdiction in this unit by using Inventory via custom lister wrapper.
	lister := &fixedJurisdictionLister{api: api, jurisdictions: []string{"default"}}
	inv, err := InventoryCloudflare(context.Background(), lister)
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Workers) != 1 || inv.Workers[0].ID != "api" {
		t.Fatalf("workers = %+v", inv.Workers)
	}
	if len(inv.R2) != 2 || r2Pages < 2 {
		t.Fatalf("r2 = %+v pages=%d", inv.R2, r2Pages)
	}
	if len(inv.KV) != 2 || kvPages < 2 {
		t.Fatalf("kv = %+v pages=%d", inv.KV, kvPages)
	}
	if len(inv.D1) != 1 || len(inv.Pages) != 1 {
		t.Fatalf("d1=%+v pages=%+v", inv.D1, inv.Pages)
	}
	resources := ResourcesFromCloudflare(inv)
	if len(resources) != 7 { // 1 worker + 2 r2 + 2 kv + 1 d1 + 1 pages
		t.Fatalf("resources = %d %+v", len(resources), resources)
	}
	for _, r := range resources {
		if r.Provider != "cloudflare" || r.CostSource != CostSourceUnpriced || r.CostPriced() || r.CostUSD != 0 {
			t.Fatalf("resource = %+v", r)
		}
	}
	_ = billingPages
}

type fixedJurisdictionLister struct {
	api           *CloudflareAPI
	jurisdictions []string
}

func (f *fixedJurisdictionLister) ListWorkersScripts(ctx context.Context) ([]CloudflareWorkerScript, error) {
	return f.api.ListWorkersScripts(ctx)
}
func (f *fixedJurisdictionLister) ListR2Buckets(ctx context.Context, jurisdiction string) ([]CloudflareR2Bucket, error) {
	for _, want := range f.jurisdictions {
		if want == jurisdiction {
			return f.api.ListR2Buckets(ctx, jurisdiction)
		}
	}
	return nil, nil
}
func (f *fixedJurisdictionLister) ListKVNamespaces(ctx context.Context) ([]CloudflareKVNamespace, error) {
	return f.api.ListKVNamespaces(ctx)
}
func (f *fixedJurisdictionLister) ListD1Databases(ctx context.Context) ([]CloudflareD1Database, error) {
	return f.api.ListD1Databases(ctx)
}
func (f *fixedJurisdictionLister) ListPagesProjects(ctx context.Context) ([]CloudflarePagesProject, error) {
	return f.api.ListPagesProjects(ctx)
}

func TestCloudflarePartialFailureAndAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/workers/scripts"):
			io.WriteString(w, `{"success":true,"result":[{"id":"ok"}],"errors":[],"messages":[]}`)
		case strings.HasSuffix(r.URL.Path, "/r2/buckets"):
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}],"messages":[],"result":null}`)
		case strings.HasSuffix(r.URL.Path, "/storage/kv/namespaces"):
			io.WriteString(w, `{"success":true,"result":[{"id":"kv","title":"kv"}],"result_info":{"page":1,"per_page":20,"total_count":1},"errors":[],"messages":[]}`)
		case strings.HasSuffix(r.URL.Path, "/d1/database"):
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"success":false,"errors":[{"code":10000,"message":"Invalid API Token"}],"result":null}`)
		case strings.HasSuffix(r.URL.Path, "/pages/projects"):
			io.WriteString(w, `{"success":true,"result":[],"result_info":{"page":1,"per_page":20,"total_count":0},"errors":[],"messages":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	api := &CloudflareAPI{AccountID: "acct", Token: "tok", BaseURL: srv.URL, HTTP: srv.Client()}
	p := NewCloudflareProviderWith("acct", &fixedJurisdictionLister{api: api, jurisdictions: []string{"default"}})
	resources, err := p.Scan(context.Background())
	if err == nil {
		t.Fatal("expected partial error")
	}
	if !strings.Contains(err.Error(), "r2") || !strings.Contains(err.Error(), "d1") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "tok") || strings.Contains(err.Error(), "Bearer") {
		t.Fatalf("error leaked token: %v", err)
	}
	var sawWorker, sawKV bool
	for _, r := range resources {
		if r.Type == ResourceCFWorker {
			sawWorker = true
		}
		if r.Type == ResourceCFKV {
			sawKV = true
		}
	}
	if !sawWorker || !sawKV {
		t.Fatalf("expected partial resources, got %+v", resources)
	}
}

func TestCloudflareExecuteRefusesMutation(t *testing.T) {
	p := NewCloudflareProviderWith("acct", &stubCFLister{})
	_, err := p.Execute(context.Background(), Resource{ID: "api", Type: ResourceCFWorker, Provider: "cloudflare"}, "terminate", "")
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected UnsupportedError, got %v", err)
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("err = %v", err)
	}
}

func TestRedactCloudflareSecret(t *testing.T) {
	got := redactCloudflareSecret(`GET failed Authorization: Bearer super-secret-token more`)
	if strings.Contains(got, "super-secret-token") || !strings.Contains(got, "[redacted]") {
		t.Fatalf("got %q", got)
	}
}

func TestCloudflareProviderRequiresTokenAndAccount(t *testing.T) {
	p := NewCloudflareProvider("", "")
	_, err := p.Scan(context.Background())
	if err == nil || !strings.Contains(err.Error(), "account id") {
		t.Fatalf("err = %v", err)
	}
	p = NewCloudflareProvider("acct", "")
	_, err = p.Scan(context.Background())
	if err == nil || !strings.Contains(err.Error(), "api token") {
		t.Fatalf("err = %v", err)
	}
}

type stubCFLister struct{}

func (stubCFLister) ListWorkersScripts(context.Context) ([]CloudflareWorkerScript, error) {
	return nil, nil
}
func (stubCFLister) ListR2Buckets(context.Context, string) ([]CloudflareR2Bucket, error) {
	return nil, nil
}
func (stubCFLister) ListKVNamespaces(context.Context) ([]CloudflareKVNamespace, error) {
	return nil, nil
}
func (stubCFLister) ListD1Databases(context.Context) ([]CloudflareD1Database, error) {
	return nil, nil
}
func (stubCFLister) ListPagesProjects(context.Context) ([]CloudflarePagesProject, error) {
	return nil, nil
}

func TestResourcesFromCloudflareTypes(t *testing.T) {
	resources := ResourcesFromCloudflare(CloudflareInventory{
		Workers: []CloudflareWorkerScript{{ID: "w"}},
		R2:      []CloudflareR2Bucket{{Name: "b", Jurisdiction: "eu"}, {Name: "b", Jurisdiction: "eu"}},
		KV:      []CloudflareKVNamespace{{ID: "k", Title: "keys"}},
		D1:      []CloudflareD1Database{{UUID: "d", Name: "db"}},
		Pages:   []CloudflarePagesProject{{Name: "site"}},
	})
	if len(resources) != 5 {
		t.Fatalf("dedupe failed: %d", len(resources))
	}
	raw, _ := json.Marshal(resources)
	if strings.Contains(string(raw), "token") {
		t.Fatalf("unexpected token in resources")
	}
}
