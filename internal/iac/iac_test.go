package iac

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndrewGrayYouNeeK/cloudripper/internal/cloud"
)

func TestLoadTerraformPulumiAndTags(t *testing.T) {
	dir := t.TempDir()
	tf := `{
	  "resources": [{
	    "type": "aws_instance",
	    "name": "web",
	    "instances": [{"attributes": {"id": "i-tf", "arn": "arn:aws:ec2:us-east-1:1:instance/i-tf"}}]
	  }]
	}`
	if err := os.WriteFile(filepath.Join(dir, "dev.tfstate"), []byte(tf), 0o644); err != nil {
		t.Fatal(err)
	}
	pulumi := `{
	  "deployment": {
	    "resources": [
	      {"urn": "urn:pulumi:dev::app::pulumi:pulumi:Stack::app-dev", "type": "pulumi:pulumi:Stack", "custom": false},
	      {"urn": "urn:pulumi:dev::app::gcp:compute/instance:Instance::api", "type": "gcp:compute/instance:Instance", "id": "123", "custom": true}
	    ]
	  }
	}`
	if err := os.WriteFile(filepath.Join(dir, "pulumi.dev.json"), []byte(pulumi), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, ok := idx.Lookup("i-tf", nil)
	if !ok || info.Tool != "terraform" || info.Address != "aws_instance.web" {
		t.Fatalf("terraform = %+v %v", info, ok)
	}
	info, ok = idx.Lookup("123", nil)
	if !ok || info.Tool != "pulumi" || info.Stack != "dev" {
		t.Fatalf("pulumi = %+v %v", info, ok)
	}
	info, ok = idx.Lookup("other", map[string]string{"ManagedBy": "Terraform"})
	if !ok || info.Tool != "terraform" || info.Source != "tag" {
		t.Fatalf("tag = %+v %v", info, ok)
	}
	empty, err := Load("")
	if err != nil || empty == nil {
		t.Fatalf("empty load = %v %v", empty, err)
	}
}

func TestWritePlan(t *testing.T) {
	dir := t.TempDir()
	path, err := WritePlan(dir, cloud.Resource{Provider: "aws", ID: "i-tf", Name: "web"}, Info{Tool: "terraform", Address: "aws_instance.web", Source: "state"}, "rightsize", "t3.small")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "t3.small") || !strings.Contains(string(body), "aws_instance") {
		t.Fatalf("plan = %s", body)
	}
}
