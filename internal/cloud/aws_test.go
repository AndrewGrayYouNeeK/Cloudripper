package cloud

import "testing"

func TestEstimateEC2CostRunning(t *testing.T) {
	running := ec2RunningCost("t3.micro")
	got := estimateEC2Cost("t3.micro", "running")
	if got != running {
		t.Fatalf("expected running cost %f, got %f", running, got)
	}
}

func TestEstimateEC2CostStopped(t *testing.T) {
	running := ec2RunningCost("t3.micro")
	stopped := estimateEC2Cost("t3.micro", "stopped")
	want := running * stoppedStorageCostFactor

	if stopped != want {
		t.Fatalf("expected stopped cost %f, got %f", want, stopped)
	}
	if stopped >= running {
		t.Fatalf("stopped cost %f should be less than running cost %f", stopped, running)
	}
}

func TestEstimateEC2CostTerminated(t *testing.T) {
	if cost := estimateEC2Cost("t3.micro", "terminated"); cost != 0 {
		t.Fatalf("expected 0 cost for terminated instance, got %f", cost)
	}
	if cost := estimateEC2Cost("t3.micro", "shutting-down"); cost != 0 {
		t.Fatalf("expected 0 cost for shutting-down instance, got %f", cost)
	}
}
