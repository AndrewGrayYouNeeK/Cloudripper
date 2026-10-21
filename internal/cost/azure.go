package cost

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/costmanagement/armcostmanagement"
)

// AzureQuery is the Cost Management usage call.
type AzureQuery interface {
	Usage(ctx context.Context, scope string, parameters armcostmanagement.QueryDefinition, options *armcostmanagement.QueryClientUsageOptions) (armcostmanagement.QueryClientUsageResponse, error)
}

// AzureCost reads subscription spend from Cost Management.
type AzureCost struct {
	API            AzureQuery
	SubscriptionID string
	ResourceGroup  string
	Now            func() time.Time
}

// NewAzureCost builds a Cost Management client with DefaultAzureCredential.
func NewAzureCost(subscription, resourceGroup string) (*AzureCost, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, err
	}
	client, err := armcostmanagement.NewQueryClient(cred, nil)
	if err != nil {
		return nil, err
	}
	return &AzureCost{API: client, SubscriptionID: subscription, ResourceGroup: resourceGroup}, nil
}

// Name implements Source.
func (a *AzureCost) Name() string { return cloud.CostSourceAzure }

// Fetch implements Source.
func (a *AzureCost) Fetch(ctx context.Context) (Snapshot, error) {
	if a == nil || a.API == nil {
		return Snapshot{}, fmt.Errorf("azure cost client not configured")
	}
	if a.SubscriptionID == "" {
		return Snapshot{}, fmt.Errorf("azure subscription not configured")
	}
	nowFn := time.Now
	if a.Now != nil {
		nowFn = a.Now
	}
	start, end := Window(nowFn())
	scope := "/subscriptions/" + a.SubscriptionID
	def := armcostmanagement.QueryDefinition{
		Type:      to.Ptr(armcostmanagement.ExportTypeActualCost),
		Timeframe: to.Ptr(armcostmanagement.TimeframeTypeCustom),
		TimePeriod: &armcostmanagement.QueryTimePeriod{
			From: &start,
			To:   &end,
		},
		Dataset: &armcostmanagement.QueryDataset{
			Granularity: to.Ptr(armcostmanagement.GranularityTypeDaily),
			Aggregation: map[string]*armcostmanagement.QueryAggregation{
				"totalCost": {
					Name:     to.Ptr("Cost"),
					Function: to.Ptr(armcostmanagement.FunctionTypeSum),
				},
			},
			Grouping: []*armcostmanagement.QueryGrouping{
				{Type: to.Ptr(armcostmanagement.QueryColumnTypeDimension), Name: to.Ptr("ServiceName")},
				{Type: to.Ptr(armcostmanagement.QueryColumnTypeDimension), Name: to.Ptr("ResourceId")},
			},
		},
	}
	if a.ResourceGroup != "" {
		def.Dataset.Filter = &armcostmanagement.QueryFilter{
			Dimensions: &armcostmanagement.QueryComparisonExpression{
				Name:     to.Ptr("ResourceGroupName"),
				Operator: to.Ptr(armcostmanagement.QueryOperatorTypeIn),
				Values:   []*string{to.Ptr(a.ResourceGroup)},
			},
		}
	}
	resp, err := a.API.Usage(ctx, scope, def, nil)
	if err != nil {
		return Snapshot{}, err
	}
	return ParseAzureQuery(resp.Properties), nil
}

// ParseAzureQuery maps a Cost Management result into a snapshot.
func ParseAzureQuery(props *armcostmanagement.QueryProperties) Snapshot {
	snap := Snapshot{Source: cloud.CostSourceAzure}
	if props == nil {
		return snap
	}
	serviceIdx, resourceIdx, costIdx := -1, -1, -1
	for i, col := range props.Columns {
		if col == nil || col.Name == nil {
			continue
		}
		switch strings.ToLower(*col.Name) {
		case "servicename":
			serviceIdx = i
		case "resourceid":
			resourceIdx = i
		case "cost", "pretaxcost", "totalcost":
			costIdx = i
		}
	}
	if costIdx < 0 {
		snap.Warnings = append(snap.Warnings, "azure cost result did not include a cost column")
		return snap
	}
	for _, row := range props.Rows {
		if costIdx >= len(row) {
			continue
		}
		usd, ok := anyFloat(row[costIdx])
		if !ok {
			continue
		}
		service := ""
		if serviceIdx >= 0 && serviceIdx < len(row) {
			service = fmt.Sprint(row[serviceIdx])
		}
		resource := ""
		if resourceIdx >= 0 && resourceIdx < len(row) && row[resourceIdx] != nil {
			resource = fmt.Sprint(row[resourceIdx])
		}
		snap.TotalUSD += usd
		if service != "" && service != "<nil>" {
			snap.Services = append(snap.Services, ServiceCost{Service: service, USD: usd})
		}
		if resource != "" && resource != "<nil>" {
			snap.Resources = append(snap.Resources, ResourceCost{ResourceID: resource, Service: service, USD: usd})
		}
	}
	return snap
}

func anyFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := ParseUSD(n, "USD")
		return f, err == nil
	default:
		f, err := ParseUSD(fmt.Sprint(v), "USD")
		return f, err == nil
	}
}
