package cloud

import "strings"

const stoppedStorageCostFactor = 0.1 // stopped EC2 bills EBS only, ~10% of running compute

// List-price estimates used when a billing API is unavailable.
const (
	EIPUnattachedMonthly   = 3.65
	ELBBaseMonthly         = 16.43
	EKSControlPlaneMonthly = 73.0
	GKEManagementMonthly   = 73.0
	GCPUnusedIPMonthly     = 7.30
	AzureUnusedIPMonthly   = 3.65
	AzureLBMonthly         = 18.0
)

func estimateEC2Cost(instanceType, status string) float64 {
	runningCost := ec2RunningCost(instanceType)
	switch strings.ToLower(status) {
	case "stopped":
		return runningCost * stoppedStorageCostFactor
	case "terminated", "shutting-down":
		return 0
	default:
		return runningCost
	}
}

func ec2RunningCost(instanceType string) float64 {
	estimates := map[string]float64{
		"t3.nano":    3.80,
		"t3.micro":   7.5,
		"t3.small":   15.0,
		"t3.medium":  30.0,
		"t3.large":   60.0,
		"t3.xlarge":  120.0,
		"m5.large":   70.0,
		"m5.xlarge":  140.0,
		"m5.2xlarge": 280.0,
		"c5.xlarge":  124.0,
		"c5.2xlarge": 248.0,
		"c5.4xlarge": 496.0,
	}
	if cost, ok := estimates[instanceType]; ok {
		return cost
	}
	return 50.0
}

// EC2ScanCost is the compute estimate used while scanning.
// When volume inventory is available, stopped instances contribute $0 here so
// attached disks are not counted twice. Otherwise it falls back to the
// historical stopped-storage factor.
func EC2ScanCost(instanceType, status string, volumesListed bool) float64 {
	if !volumesListed {
		return estimateEC2Cost(instanceType, status)
	}
	switch strings.ToLower(status) {
	case "stopped", "stopping", "terminated", "shutting-down":
		return 0
	default:
		return ec2RunningCost(instanceType)
	}
}

func estimateRDSCost(instanceClass string) float64 {
	estimates := map[string]float64{
		"db.t3.micro":  12.0,
		"db.t3.small":  24.0,
		"db.t3.medium": 48.0,
		"db.m5.large":  120.0,
		"db.m5.xlarge": 240.0,
	}
	if cost, ok := estimates[instanceClass]; ok {
		return cost
	}
	return 80.0
}

func estimateGCECost(machineTypeURL string) float64 {
	machineType := LastPathSegment(machineTypeURL)
	estimates := map[string]float64{
		"e2-micro":      6.0,
		"e2-small":      12.0,
		"e2-medium":     24.0,
		"e2-standard-2": 48.0,
		"e2-standard-4": 96.0,
		"n1-standard-1": 48.0,
		"n1-standard-2": 96.0,
		"n1-standard-4": 192.0,
	}
	if cost, ok := estimates[machineType]; ok {
		return cost
	}
	return 45.0
}

// GCEScanCost zeroes compute cost for machines that are not running so disks
// can carry the storage line item.
func GCEScanCost(machineTypeURL, status string) float64 {
	switch strings.ToUpper(status) {
	case "STOPPED", "SUSPENDED", "TERMINATED", "STOPPING", "SUSPENDING":
		return 0
	default:
		return estimateGCECost(machineTypeURL)
	}
}

func estimateAzureVMCost(size string) float64 {
	estimates := map[string]float64{
		"Standard_B1s":    7.6,
		"Standard_B2s":    30.0,
		"Standard_D2s_v3": 70.0,
		"Standard_D4s_v3": 140.0,
		"Standard_D8s_v3": 280.0,
	}
	if cost, ok := estimates[size]; ok {
		return cost
	}
	return 55.0
}

// EBSMonthly estimates gp2/gp3/io/st1/sc1 storage from provisioned size.
func EBSMonthly(volumeType string, sizeGiB int64) float64 {
	if sizeGiB < 0 {
		sizeGiB = 0
	}
	rate := 0.08
	switch strings.ToLower(volumeType) {
	case "gp2":
		rate = 0.10
	case "gp3":
		rate = 0.08
	case "io1", "io2":
		rate = 0.125
	case "st1":
		rate = 0.045
	case "sc1":
		rate = 0.015
	case "standard":
		rate = 0.05
	}
	return rate * float64(sizeGiB)
}

// GCPDiskMonthly estimates persistent-disk storage.
func GCPDiskMonthly(diskType string, sizeGb int64) float64 {
	if sizeGb < 0 {
		sizeGb = 0
	}
	kind := strings.ToLower(diskType)
	rate := 0.04
	switch {
	case strings.Contains(kind, "extreme"):
		rate = 0.125
	case strings.Contains(kind, "pd-ssd"), strings.Contains(kind, "ssd"):
		rate = 0.17
	case strings.Contains(kind, "balanced"):
		rate = 0.10
	}
	return rate * float64(sizeGb)
}

// AzureDiskMonthly is a rough managed-disk estimate.
func AzureDiskMonthly(sizeGb int64) float64 {
	if sizeGb < 0 {
		sizeGb = 0
	}
	return 0.05 * float64(sizeGb)
}
