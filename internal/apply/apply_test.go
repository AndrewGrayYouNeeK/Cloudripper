package apply

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/audit"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/iac"
	"github.com/AndrewGrayYouNeeK/cloudripper/internal/optimizer"
)

type fakeExec struct {
	calls int
}

func (f *fakeExec) Provider() string { return "aws" }

func (f *fakeExec) Execute(context.Context, cloud.Resource, string, string) (string, error) {
	f.calls++
	return "done", nil
}

func rec(id, action string, tags map[string]string) optimizer.Recommendation {
	return optimizer.Recommendation{
		Resource: cloud.Resource{ID: id, Name: id, Provider: "aws", Type: cloud.ResourceEC2, Status: "stopped", Tags: tags},
		Action:   optimizer.ActionType(action),
		Savings:  2,
	}
}

func TestExecuteDryRunDoesNotCallExecutor(t *testing.T) {
	exec := &fakeExec{}
	records, err := Execute(context.Background(), []optimizer.Recommendation{rec("i-1", "terminate", nil)}, []cloud.Executor{exec}, Options{DryRun: true, Command: "optimize"})
	if err != nil {
		t.Fatal(err)
	}
	if exec.calls != 0 || records[0].Outcome != OutcomeDryRun {
		t.Fatalf("calls %d records %+v", exec.calls, records)
	}
}

func TestExecuteConfirmation(t *testing.T) {
	exec := &fakeExec{}
	recs := []optimizer.Recommendation{rec("i-1", "terminate", nil)}
	if _, err := Execute(context.Background(), recs, []cloud.Executor{exec}, Options{}); err == nil {
		t.Fatal("expected missing confirmer error")
	}
	records, err := Execute(context.Background(), recs, []cloud.Executor{exec}, Options{Confirmer: NoConfirmer{}})
	if err != nil {
		t.Fatal(err)
	}
	if exec.calls != 0 || records[0].Outcome != OutcomeConfirm {
		t.Fatalf("calls %d records %+v", exec.calls, records)
	}
	records, err = Execute(context.Background(), recs, []cloud.Executor{exec}, Options{Confirmer: YesConfirmer{}})
	if err != nil {
		t.Fatal(err)
	}
	if exec.calls != 1 || records[0].Outcome != OutcomeApplied {
		t.Fatalf("calls %d records %+v", exec.calls, records)
	}
}

func TestExecuteProtectAndIaC(t *testing.T) {
	exec := &fakeExec{}
	dir := t.TempDir()
	index := &iac.Index{}
	loaded, err := iac.Load("")
	if err != nil {
		t.Fatal(err)
	}
	index = loaded
	records, err := Execute(context.Background(), []optimizer.Recommendation{
		rec("i-keep", "terminate", map[string]string{cloud.ProtectTag: "1"}),
		rec("i-tf", "rightsize", map[string]string{"ManagedBy": "terraform"}),
	}, []cloud.Executor{exec}, Options{
		Confirmer: YesConfirmer{},
		Index:     index,
		PlanDir:   dir,
		Audit:     &audit.Logger{Path: dir + "/audit.log"},
		Command:   "optimize",
	})
	if err != nil {
		t.Fatal(err)
	}
	if exec.calls != 0 {
		t.Fatalf("executor calls = %d", exec.calls)
	}
	if records[0].Outcome != OutcomeProtected || records[1].Outcome != OutcomeIaC {
		t.Fatalf("records = %+v", records)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var plan string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".plan.txt") {
			body, err := os.ReadFile(dir + "/" + entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			plan = string(body)
		}
	}
	if !strings.Contains(plan, "terraform") {
		t.Fatalf("plan = %s", plan)
	}
	events, err := audit.ReadAll(dir + "/audit.log")
	if err != nil || len(events) != 2 {
		t.Fatalf("events %v err %v", events, err)
	}

	records, err = Execute(context.Background(), []optimizer.Recommendation{
		rec("i-tf", "terminate", map[string]string{"ManagedBy": "terraform"}),
	}, []cloud.Executor{exec}, Options{Confirmer: YesConfirmer{}, Index: index, ForceIaC: true})
	if err != nil {
		t.Fatal(err)
	}
	if exec.calls != 1 || records[0].Outcome != OutcomeApplied {
		t.Fatalf("force records = %+v calls %d", records, exec.calls)
	}
}
