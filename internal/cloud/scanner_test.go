package cloud

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/api/compute/v1"
)

type stubProvider struct {
	name string
	res  []Resource
	err  error
}

func (s stubProvider) Name() string { return s.name }

func (s stubProvider) Scan(context.Context) ([]Resource, error) {
	return s.res, s.err
}

func TestMultiScannerReturnsPartialResults(t *testing.T) {
	scanner := NewMultiScanner(
		stubProvider{name: "aws", res: []Resource{{ID: "i-1", Provider: "aws"}}, err: errors.New("throttled")},
		stubProvider{name: "gcp", res: []Resource{{ID: "g-1", Provider: "gcp"}}},
	)
	got, err := scanner.ScanAll(context.Background())
	if err == nil || len(got) != 2 {
		t.Fatalf("got %d resources, err %v", len(got), err)
	}
}

func TestSizingProtectAndScanCost(t *testing.T) {
	smaller, ok := SmallerSpec("aws", "t3.micro")
	if !ok || smaller != "t3.nano" {
		t.Fatalf("smaller = %s %v", smaller, ok)
	}
	larger, ok := LargerSpec("aws", "https://example/machineTypes/t3.micro")
	if !ok || larger != "t3.small" {
		t.Fatalf("larger = %s %v", larger, ok)
	}
	if _, ok := SmallerSpec("aws", "no-such-size"); ok {
		t.Fatal("unknown size should not step")
	}
	if !IsProtected(map[string]string{"CloudRipper:Protect": "Yes"}) {
		t.Fatal("expected protected tag")
	}
	if IsProtected(map[string]string{ProtectTag: "false"}) {
		t.Fatal("false is not protected")
	}
	if EC2ScanCost("t3.micro", "stopped", true) != 0 {
		t.Fatal("stopped compute cost is zero when volumes are listed separately")
	}
	if EBSMonthly("gp3", 100) != 8 {
		t.Fatalf("ebs cost = %v", EBSMonthly("gp3", 100))
	}
}

func TestResourcesFromGCP(t *testing.T) {
	resources := ResourcesFromGCP("us-central1", GCPInventory{
		Instances: []*compute.Instance{{
			Id:          10,
			Name:        "web",
			Zone:        "zones/us-central1-a",
			Status:      "RUNNING",
			MachineType: "zones/us-central1-a/machineTypes/e2-micro",
		}},
		Disks: []*compute.Disk{{
			Name:   "orphan",
			Zone:   "zones/us-central1-a",
			Type:   "diskTypes/pd-standard",
			SizeGb: 20,
		}},
		ForwardingRules: []*compute.ForwardingRule{{
			Name:   "idle-rule",
			Region: "regions/us-central1",
		}},
	})
	if len(resources) != 3 {
		t.Fatalf("got %d resources", len(resources))
	}
	var disk, rule Resource
	for _, r := range resources {
		if r.Type == ResourceGCPDisk {
			disk = r
		}
		if r.Type == ResourceGCPLB {
			rule = r
		}
	}
	if disk.Status != "unattached" || disk.CostUSD <= 0 {
		t.Fatalf("disk = %+v", disk)
	}
	if rule.Status != "idle" {
		t.Fatalf("rule = %+v", rule)
	}
}

func TestResourcesFromAzure(t *testing.T) {
	resources := ResourcesFromAzure(AzureInventory{
		VMs: []AzureVM{{
			ID: "vm-1", Name: "app", Location: "eastus", Group: "rg", Size: "Standard_B1s", Status: "deallocated",
		}},
		Disks:     []AzureDisk{{ID: "disk-1", Name: "data", Location: "eastus", Group: "rg", SizeGB: 64, Status: "unattached"}},
		Addresses: []AzureAddress{{ID: "ip-1", Name: "pip", Location: "eastus", Group: "rg", Status: "unattached"}},
		Clusters:  []AzureCluster{{ID: "aks-1", Name: "cluster", Location: "eastus", Status: "Succeeded", Version: "1.29"}},
	})
	byType := map[ResourceType]Resource{}
	for _, r := range resources {
		byType[r.Type] = r
	}
	if byType[ResourceAzureVM].CostUSD != 0 {
		t.Fatalf("stopped vm cost = %v", byType[ResourceAzureVM].CostUSD)
	}
	if byType[ResourceAzureDisk].CostUSD != AzureDiskMonthly(64) {
		t.Fatalf("disk = %+v", byType[ResourceAzureDisk])
	}
	if byType[ResourceAzureIP].CostUSD != AzureUnusedIPMonthly {
		t.Fatalf("ip = %+v", byType[ResourceAzureIP])
	}
	if byType[ResourceAKS].Spec != "1.29" {
		t.Fatalf("aks = %+v", byType[ResourceAKS])
	}
}
