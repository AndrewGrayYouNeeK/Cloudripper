package cost

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/costmanagement/armcostmanagement"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

type fakeSource struct {
	name string
	snap Snapshot
	err  error
}

func (f fakeSource) Name() string { return f.name }

func (f fakeSource) Fetch(context.Context) (Snapshot, error) {
	return f.snap, f.err
}

func TestResolveOverlaysActualsAndKeepsEstimates(t *testing.T) {
	resources := []cloud.Resource{
		{ID: "i-abc", Provider: "aws", CostUSD: 1, CostSource: cloud.CostSourceEstimate},
		{ID: "g-1", Provider: "gcp", CostUSD: 2, CostSource: cloud.CostSourceEstimate},
	}
	res := Resolve(context.Background(), resources, []Source{
		fakeSource{name: cloud.CostSourceAWS, err: errors.New("denied")},
		fakeSource{name: cloud.CostSourceGCP, snap: Snapshot{
			TotalUSD: 40,
			Resources: []ResourceCost{{
				ResourceID: "g-1",
				USD:        12,
			}},
		}},
	})
	if res.EstimatedTotal != 3 || res.ActualTotal != 40 || !res.ActualAvailable {
		t.Fatalf("resolution = %+v", res)
	}
	if res.Resources[0].CostUSD != 1 {
		t.Fatalf("aws resource should keep its estimate when cost explorer fails, got %v", res.Resources[0].CostUSD)
	}
	if res.Resources[1].CostUSD != 12 || res.Resources[1].CostSource != cloud.CostSourceGCP {
		t.Fatalf("gcp resource = %+v", res.Resources[1])
	}
	if len(res.Warnings) == 0 {
		t.Fatal("expected a warning for the failed source")
	}
}

func TestBillingSQLRejectsInjection(t *testing.T) {
	if _, err := BillingSQL("p.d.t; drop table x", time.Now()); err == nil {
		t.Fatal("expected invalid table error")
	}
	sql, err := BillingSQL("billing-project.dataset.gcp_billing_export_v1", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, ";") || !strings.Contains(sql, "`billing-project.dataset.gcp_billing_export_v1`") {
		t.Fatalf("sql = %s", sql)
	}
	if !strings.Contains(sql, "UNNEST(credits)") {
		t.Fatal("sql should net credits")
	}
}

type fakeExplorer struct {
	serviceErr  error
	resourceErr error
	region      string
}

func (f *fakeExplorer) GetCostAndUsage(_ context.Context, params *costexplorer.GetCostAndUsageInput, _ ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error) {
	if f.serviceErr != nil {
		return nil, f.serviceErr
	}
	if params.Filter != nil && params.Filter.Dimensions != nil && len(params.Filter.Dimensions.Values) > 0 {
		f.region = params.Filter.Dimensions.Values[0]
	}
	return &costexplorer.GetCostAndUsageOutput{
		ResultsByTime: []cetypes.ResultByTime{{
			Groups: []cetypes.Group{{
				Keys: []string{"AmazonEC2"},
				Metrics: map[string]cetypes.MetricValue{
					"UnblendedCost": {Amount: aws.String("10.50"), Unit: aws.String("USD")},
				},
			}},
		}},
	}, nil
}

func (f *fakeExplorer) GetCostAndUsageWithResources(context.Context, *costexplorer.GetCostAndUsageWithResourcesInput, ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageWithResourcesOutput, error) {
	if f.resourceErr != nil {
		return nil, f.resourceErr
	}
	return &costexplorer.GetCostAndUsageWithResourcesOutput{}, nil
}

func TestAWSExplorerKeepsServiceTotalsWhenResourceQueryFails(t *testing.T) {
	explorer := &AWSExplorer{
		API:    &fakeExplorer{resourceErr: errors.New("not enabled")},
		Region: "us-west-2",
		Now:    func() time.Time { return time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) },
	}
	snap, err := explorer.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.TotalUSD != 10.50 {
		t.Fatalf("total = %v", snap.TotalUSD)
	}
	if len(snap.Warnings) == 0 {
		t.Fatal("expected resource-level warning")
	}
	api := explorer.API.(*fakeExplorer)
	if api.region != "us-west-2" {
		t.Fatalf("region filter = %s", api.region)
	}
}

func TestEmptyRegionOmitsCostExplorerFilter(t *testing.T) {
	api := &fakeExplorer{}
	explorer := &AWSExplorer{
		API:    api,
		Region: "",
		Now:    func() time.Time { return time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) },
	}
	if _, err := explorer.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if api.region != "" {
		t.Fatalf("region filter = %q", api.region)
	}
}

func TestCostExplorerDisabledHint(t *testing.T) {
	explorer := &AWSExplorer{
		API: &fakeExplorer{serviceErr: errors.New("User not enabled for cost explorer access")},
		Now: func() time.Time { return time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) },
	}
	_, err := explorer.Fetch(context.Background())
	if err == nil || strings.Contains(err.Error(), "User not enabled") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "Billing console") || !strings.Contains(err.Error(), "24 hours") {
		t.Fatalf("err = %v", err)
	}
}

func TestSummaryOmitsWarnings(t *testing.T) {
	text := Summary(Resolution{
		EstimatedTotal: 3,
		Warnings:       []string{"Cost Explorer is not enabled for this account. Enable it in the AWS Billing console; cost data usually appears after about 24 hours."},
	})
	if strings.Contains(text, "warning:") || strings.Contains(text, "Cost Explorer") {
		t.Fatalf("summary repeated the warning:\n%s", text)
	}
}

func TestParseAzureQuery(t *testing.T) {
	service := "Virtual Machines"
	resource := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/app"
	costCol := "PreTaxCost"
	resourceCol := "ResourceId"
	serviceCol := "ServiceName"
	snap := ParseAzureQuery(&armcostmanagement.QueryProperties{
		Columns: []*armcostmanagement.QueryColumn{
			{Name: &serviceCol},
			{Name: &resourceCol},
			{Name: &costCol},
		},
		Rows: [][]any{{service, resource, 12.5}},
	})
	if snap.TotalUSD != 12.5 || len(snap.Resources) != 1 || snap.Resources[0].ResourceID != resource {
		t.Fatalf("snap = %+v", snap)
	}
}
