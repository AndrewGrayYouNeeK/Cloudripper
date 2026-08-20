package cloud

import "testing"

func TestEstimateEC2CostStopped(t *testing.T) {
	running := ec2RunningCost("t3.micro")
	stopped := estimateEC2Cost("t3.micro", "stopped")
	if stopped >= running {
		t.Fatalf("stopped cost %f should be less than running cost %f", stopped, running)
	}
}

func TestEstimateEC2CostTerminated(t *testing.T) {
	if cost := estimateEC2Cost("t3.micro", "terminated"); cost != 0 {
		t.Fatalf("expected 0 cost for terminated instance, got %f", cost)
	}
}
