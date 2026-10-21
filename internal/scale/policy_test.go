package scale

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDecideBoundariesAndCooldown(t *testing.T) {
	min, max := 1, 3
	policy := Policy{Provider: "aws", Kind: KindASG, Target: "web", Min: &min, Max: &max}
	group := Group{Provider: "aws", Kind: KindASG, Name: "web", Desired: 2, Min: 1, Max: 10, Spec: "t3.small"}
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

	out, err := Decide(policy, group, 80, time.Time{}, now)
	if err != nil || out.Action != ActionScaleOut || out.To != 3 {
		t.Fatalf("scale out = %+v %v", out, err)
	}
	group.Desired = 3
	out, err = Decide(policy, group, 90, time.Time{}, now)
	if err != nil || out.Action != ActionNone {
		t.Fatalf("at max = %+v %v", out, err)
	}
	group.Desired = 2
	out, err = Decide(policy, group, 10, time.Time{}, now)
	if err != nil || out.Action != ActionScaleIn || out.To != 1 {
		t.Fatalf("scale in = %+v %v", out, err)
	}
	out, err = Decide(policy, group, 90, now.Add(-time.Minute), now)
	if err != nil || out.Action != ActionNone || out.Reason != "cooldown" {
		t.Fatalf("cooldown = %+v %v", out, err)
	}

	vertical := Policy{Provider: "aws", Kind: KindEC2, Target: "i-1"}
	out, err = Decide(vertical, Group{Spec: "t3.small"}, 95, time.Time{}, now)
	if err != nil || out.Action != ActionResizeUp {
		t.Fatalf("resize = %+v %v", out, err)
	}
}

func TestLoadFileAndRunCooldown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	body := []byte(`{"policies":[{"name":"web","provider":"aws","kind":"asg","target":"web","min":1,"max":4,"cooldown":"5m"}]}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	policies, err := LoadFile(path)
	if err != nil || len(policies) != 1 || policies[0].Cooldown != 5*time.Minute {
		t.Fatalf("policies = %+v %v", policies, err)
	}
	ctrl := &fakeController{group: Group{Provider: "aws", Kind: KindASG, Name: "web", Desired: 2, Min: 1, Max: 4}}
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	state := filepath.Join(dir, "state.json")
	results, err := Run(context.Background(), []Controller{ctrl}, RunOptions{
		Policies:  policies,
		Confirmer: yesConfirm{},
		StatePath: state,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if ctrl.applied != 1 || results[0].Outcome != "applied" || results[0].To != 3 {
		t.Fatalf("first = %+v applied %d", results, ctrl.applied)
	}
	results, err = Run(context.Background(), []Controller{ctrl}, RunOptions{
		Policies:  policies,
		Confirmer: yesConfirm{},
		StatePath: state,
		Now:       func() time.Time { return now.Add(time.Minute) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if ctrl.applied != 1 || results[0].Action != ActionNone {
		t.Fatalf("second = %+v applied %d", results, ctrl.applied)
	}
}

func TestRunDryRunDoesNotApply(t *testing.T) {
	ctrl := &fakeController{group: Group{Provider: "aws", Kind: KindASG, Name: "web", Desired: 1, Min: 1, Max: 3}}
	results, err := Run(context.Background(), []Controller{ctrl}, RunOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if ctrl.applied != 0 || results[0].Outcome != "dry-run" {
		t.Fatalf("results %+v applied %d", results, ctrl.applied)
	}
}

type yesConfirm struct{}

func (yesConfirm) Confirm(string) (bool, error) { return true, nil }

type fakeController struct {
	group   Group
	applied int
}

func (f *fakeController) Provider() string { return "aws" }

func (f *fakeController) Discover(context.Context) ([]Group, error) {
	return []Group{f.group}, nil
}

func (f *fakeController) Lookup(context.Context, string, string) (Group, error) {
	return f.group, nil
}

func (f *fakeController) CPU(context.Context, Group) (float64, error) { return 90, nil }

func (f *fakeController) Apply(context.Context, Group, Decision) error {
	f.applied++
	f.group.Desired++
	return nil
}
