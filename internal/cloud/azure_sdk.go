package cloud

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
)

type azureSDK struct {
	subscription string
	group        string
	vms          *armcompute.VirtualMachinesClient
	disks        *armcompute.DisksClient
	ips          *armnetwork.PublicIPAddressesClient
	lbs          *armnetwork.LoadBalancersClient
	storage      *armstorage.AccountsClient
	web          *armappservice.WebAppsClient
	aks          *armcontainerservice.ManagedClustersClient
}

func connectAzure(ctx context.Context, subscription, group string) (AzureLister, AzureActions, error) {
	if subscription == "" {
		return nil, nil, fmt.Errorf("azure subscription not configured (set AZURE_SUBSCRIPTION_ID)")
	}
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, nil, fmt.Errorf("azure credentials: %w", err)
	}
	sdk := &azureSDK{subscription: subscription, group: group}
	if sdk.vms, err = armcompute.NewVirtualMachinesClient(subscription, cred, nil); err != nil {
		return nil, nil, err
	}
	if sdk.disks, err = armcompute.NewDisksClient(subscription, cred, nil); err != nil {
		return nil, nil, err
	}
	if sdk.ips, err = armnetwork.NewPublicIPAddressesClient(subscription, cred, nil); err != nil {
		return nil, nil, err
	}
	if sdk.lbs, err = armnetwork.NewLoadBalancersClient(subscription, cred, nil); err != nil {
		return nil, nil, err
	}
	if sdk.storage, err = armstorage.NewAccountsClient(subscription, cred, nil); err != nil {
		return nil, nil, err
	}
	if sdk.web, err = armappservice.NewWebAppsClient(subscription, cred, nil); err != nil {
		return nil, nil, err
	}
	if sdk.aks, err = armcontainerservice.NewManagedClustersClient(subscription, cred, nil); err != nil {
		return nil, nil, err
	}
	_ = ctx
	return sdk, sdk, nil
}

func (a *azureSDK) allow(group string) bool {
	return a.group == "" || strings.EqualFold(a.group, group)
}

func (a *azureSDK) Inventory(ctx context.Context) (AzureInventory, error) {
	var inv AzureInventory
	var errs []error
	pager := a.vms.NewListAllPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("vms: %w", err))
			break
		}
		for _, vm := range page.Value {
			if vm == nil {
				continue
			}
			item := AzureVM{
				ID:       deref(vm.ID),
				Name:     deref(vm.Name),
				Location: deref(vm.Location),
				Group:    ResourceGroup(deref(vm.ID)),
				Tags:     azureTags(vm.Tags),
			}
			if !a.allow(item.Group) {
				continue
			}
			if vm.Properties != nil && vm.Properties.HardwareProfile != nil && vm.Properties.HardwareProfile.VMSize != nil {
				item.Size = string(*vm.Properties.HardwareProfile.VMSize)
			}
			item.Status = a.vmStatus(ctx, item.Group, item.Name)
			inv.VMs = append(inv.VMs, item)
		}
	}
	diskPager := a.disks.NewListPager(nil)
	for diskPager.More() {
		page, err := diskPager.NextPage(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("disks: %w", err))
			break
		}
		for _, disk := range page.Value {
			if disk == nil {
				continue
			}
			item := AzureDisk{
				ID:       deref(disk.ID),
				Name:     deref(disk.Name),
				Location: deref(disk.Location),
				Group:    ResourceGroup(deref(disk.ID)),
				Status:   "in-use",
				Tags:     azureTags(disk.Tags),
			}
			if !a.allow(item.Group) {
				continue
			}
			if disk.Properties != nil {
				if disk.Properties.DiskSizeGB != nil {
					item.SizeGB = int64(*disk.Properties.DiskSizeGB)
				}
				if disk.Properties.DiskState != nil && *disk.Properties.DiskState == armcompute.DiskStateUnattached {
					item.Status = "unattached"
				}
			}
			inv.Disks = append(inv.Disks, item)
		}
	}
	ipPager := a.ips.NewListAllPager(nil)
	for ipPager.More() {
		page, err := ipPager.NextPage(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("public ips: %w", err))
			break
		}
		for _, ip := range page.Value {
			if ip == nil {
				continue
			}
			item := AzureAddress{
				ID:       deref(ip.ID),
				Name:     deref(ip.Name),
				Location: deref(ip.Location),
				Group:    ResourceGroup(deref(ip.ID)),
				Status:   "in-use",
				Tags:     azureTags(ip.Tags),
			}
			if !a.allow(item.Group) {
				continue
			}
			if ip.Properties == nil || ip.Properties.IPConfiguration == nil {
				item.Status = "unattached"
			}
			inv.Addresses = append(inv.Addresses, item)
		}
	}
	lbPager := a.lbs.NewListAllPager(nil)
	for lbPager.More() {
		page, err := lbPager.NextPage(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("load balancers: %w", err))
			break
		}
		for _, lb := range page.Value {
			if lb == nil {
				continue
			}
			item := AzureLB{
				ID:       deref(lb.ID),
				Name:     deref(lb.Name),
				Location: deref(lb.Location),
				Group:    ResourceGroup(deref(lb.ID)),
				Status:   "idle",
				Tags:     azureTags(lb.Tags),
			}
			if !a.allow(item.Group) {
				continue
			}
			if lb.Properties != nil {
				for _, pool := range lb.Properties.BackendAddressPools {
					if pool == nil || pool.Properties == nil {
						continue
					}
					if len(pool.Properties.BackendIPConfigurations) > 0 || len(pool.Properties.LoadBalancerBackendAddresses) > 0 {
						item.Status = "active"
					}
				}
			}
			inv.LoadBalancers = append(inv.LoadBalancers, item)
		}
	}
	stPager := a.storage.NewListPager(nil)
	for stPager.More() {
		page, err := stPager.NextPage(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("storage: %w", err))
			break
		}
		for _, account := range page.Value {
			if account == nil {
				continue
			}
			item := AzureStorage{
				ID:       deref(account.ID),
				Name:     deref(account.Name),
				Location: deref(account.Location),
				Group:    ResourceGroup(deref(account.ID)),
				Tags:     azureTags(account.Tags),
			}
			if account.SKU != nil && account.SKU.Name != nil {
				item.SKU = string(*account.SKU.Name)
			}
			if !a.allow(item.Group) {
				continue
			}
			inv.Storage = append(inv.Storage, item)
		}
	}
	webPager := a.web.NewListPager(nil)
	for webPager.More() {
		page, err := webPager.NextPage(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("functions: %w", err))
			break
		}
		for _, site := range page.Value {
			if site == nil || site.Kind == nil || !strings.Contains(strings.ToLower(*site.Kind), "functionapp") {
				continue
			}
			item := AzureFunction{
				ID:       deref(site.ID),
				Name:     deref(site.Name),
				Location: deref(site.Location),
				Group:    ResourceGroup(deref(site.ID)),
				Tags:     azureTags(site.Tags),
			}
			if site.Properties != nil {
				item.Status = deref(site.Properties.State)
			}
			if !a.allow(item.Group) {
				continue
			}
			inv.Functions = append(inv.Functions, item)
		}
	}
	aksPager := a.aks.NewListPager(nil)
	for aksPager.More() {
		page, err := aksPager.NextPage(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("aks: %w", err))
			break
		}
		for _, cluster := range page.Value {
			if cluster == nil {
				continue
			}
			item := AzureCluster{
				ID:       deref(cluster.ID),
				Name:     deref(cluster.Name),
				Location: deref(cluster.Location),
				Group:    ResourceGroup(deref(cluster.ID)),
				Tags:     azureTags(cluster.Tags),
			}
			if cluster.Properties != nil {
				item.Status = deref(cluster.Properties.ProvisioningState)
				item.Version = deref(cluster.Properties.KubernetesVersion)
			}
			if !a.allow(item.Group) {
				continue
			}
			inv.Clusters = append(inv.Clusters, item)
		}
	}
	if len(errs) > 0 && len(inv.VMs) == 0 && len(inv.Disks) == 0 {
		return inv, errs[0]
	}
	if len(errs) > 0 {
		return inv, fmt.Errorf("partial azure scan: %v", errs[0])
	}
	return inv, nil
}

func (a *azureSDK) vmStatus(ctx context.Context, group, name string) string {
	if group == "" || name == "" {
		return "unknown"
	}
	view, err := a.vms.InstanceView(ctx, group, name, nil)
	if err != nil {
		return "unknown"
	}
	for _, st := range view.Statuses {
		if st == nil || st.Code == nil {
			continue
		}
		if strings.HasPrefix(*st.Code, "PowerState/") {
			return strings.TrimPrefix(*st.Code, "PowerState/")
		}
	}
	return "unknown"
}

func (a *azureSDK) DeallocateVM(ctx context.Context, group, name string) error {
	poller, err := a.vms.BeginDeallocate(ctx, group, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func (a *azureSDK) StartVM(ctx context.Context, group, name string) error {
	poller, err := a.vms.BeginStart(ctx, group, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func (a *azureSDK) DeleteVM(ctx context.Context, group, name string) error {
	poller, err := a.vms.BeginDelete(ctx, group, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func (a *azureSDK) ResizeVM(ctx context.Context, group, name, size string) error {
	sizeType := armcompute.VirtualMachineSizeTypes(size)
	poller, err := a.vms.BeginUpdate(ctx, group, name, armcompute.VirtualMachineUpdate{
		Properties: &armcompute.VirtualMachineProperties{
			HardwareProfile: &armcompute.HardwareProfile{VMSize: &sizeType},
		},
	}, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func (a *azureSDK) TagVM(ctx context.Context, group, name, key, value string) error {
	existing, err := a.vms.Get(ctx, group, name, nil)
	if err != nil {
		return err
	}
	tags := map[string]*string{}
	for k, v := range existing.Tags {
		tags[k] = v
	}
	tags[key] = to.Ptr(value)
	poller, err := a.vms.BeginUpdate(ctx, group, name, armcompute.VirtualMachineUpdate{Tags: tags}, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func (a *azureSDK) DeleteDisk(ctx context.Context, group, name string) error {
	poller, err := a.disks.BeginDelete(ctx, group, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func (a *azureSDK) DeleteAddress(ctx context.Context, group, name string) error {
	poller, err := a.ips.BeginDelete(ctx, group, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func (a *azureSDK) DeleteLoadBalancer(ctx context.Context, group, name string) error {
	poller, err := a.lbs.BeginDelete(ctx, group, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func azureTags(tags map[string]*string) map[string]string {
	if len(tags) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(tags))
	for k, v := range tags {
		out[k] = deref(v)
	}
	return out
}
