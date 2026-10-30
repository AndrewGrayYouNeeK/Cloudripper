package iac

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AndrewGrayYouNeeK/Cloudripper/internal/cloud"
)

// Info describes infrastructure-as-code ownership of a resource.
type Info struct {
	Tool    string `json:"tool"`
	Address string `json:"address,omitempty"`
	Stack   string `json:"stack,omitempty"`
	Source  string `json:"source"`
}

// Index maps resource IDs to IaC ownership discovered from state files.
type Index struct {
	byID map[string]Info
}

// Lookup prefers state-file ownership, then tag heuristics.
func (idx *Index) Lookup(id string, tags map[string]string) (Info, bool) {
	if idx != nil {
		if info, ok := idx.byID[id]; ok {
			return info, true
		}
		if info, ok := idx.byID[strings.ToLower(id)]; ok {
			return info, true
		}
	}
	return FromTags(tags)
}

// FromTags detects Terraform or Pulumi ownership from resource tags.
func FromTags(tags map[string]string) (Info, bool) {
	if len(tags) == 0 {
		return Info{}, false
	}
	var managed string
	for k, v := range tags {
		lk := strings.ToLower(strings.TrimSpace(k))
		lv := strings.ToLower(strings.TrimSpace(v))
		if strings.HasPrefix(lk, "pulumi:") {
			return Info{
				Tool:    "pulumi",
				Source:  "tag",
				Stack:   firstNonEmpty(tags["pulumi:stack"], tags["pulumi:project"]),
				Address: tags["pulumi:urn"],
			}, true
		}
		if lk == "managedby" || lk == "managed-by" || lk == "managed_by" {
			managed = lv
		}
		if lk == "terraform" && (lv == "true" || lv == "1" || lv == "yes") {
			return Info{Tool: "terraform", Source: "tag"}, true
		}
	}
	switch {
	case strings.Contains(managed, "pulumi"):
		return Info{Tool: "pulumi", Source: "tag"}, true
	case strings.Contains(managed, "terraform"):
		return Info{Tool: "terraform", Source: "tag"}, true
	default:
		return Info{}, false
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Load reads a Terraform state file, a Pulumi export, or a directory of both.
func Load(path string) (*Index, error) {
	idx := &Index{byID: map[string]Info{}}
	if strings.TrimSpace(path) == "" {
		return idx, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("iac state: %w", err)
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := strings.ToLower(entry.Name())
			if strings.HasSuffix(name, ".tfstate") || strings.Contains(name, "pulumi") && strings.HasSuffix(name, ".json") {
				if err := idx.loadFile(filepath.Join(path, entry.Name())); err != nil {
					return nil, err
				}
			}
		}
		return idx, nil
	}
	if err := idx.loadFile(path); err != nil {
		return nil, err
	}
	return idx, nil
}

func (idx *Index) loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if strings.Contains(string(data), `"deployment"`) && strings.Contains(string(data), `"urn"`) {
		return idx.loadPulumi(data)
	}
	return idx.loadTerraform(data)
}

func (idx *Index) add(id string, info Info) {
	id = strings.TrimSpace(id)
	if id == "" || id == "null" {
		return
	}
	idx.byID[id] = info
	idx.byID[strings.ToLower(id)] = info
}

type tfState struct {
	Resources []tfResource `json:"resources"`
}

type tfResource struct {
	Module    string `json:"module"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Instances []struct {
		Attributes map[string]any `json:"attributes"`
	} `json:"instances"`
}

func (idx *Index) loadTerraform(data []byte) error {
	var state tfState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("terraform state: %w", err)
	}
	for _, res := range state.Resources {
		address := res.Type + "." + res.Name
		if res.Module != "" {
			address = res.Module + "." + address
		}
		info := Info{Tool: "terraform", Address: address, Source: "state"}
		for _, inst := range res.Instances {
			idx.add(stringAttr(inst.Attributes, "id"), info)
			idx.add(stringAttr(inst.Attributes, "arn"), info)
			idx.add(cloud.LastPathSegment(stringAttr(inst.Attributes, "id")), info)
		}
	}
	return nil
}

func stringAttr(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	v, ok := attrs[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprint(v)
	}
	return s
}

type pulumiExport struct {
	Deployment struct {
		Resources []struct {
			URN    string `json:"urn"`
			Type   string `json:"type"`
			ID     string `json:"id"`
			Custom bool   `json:"custom"`
		} `json:"resources"`
	} `json:"deployment"`
}

func (idx *Index) loadPulumi(data []byte) error {
	var exp pulumiExport
	if err := json.Unmarshal(data, &exp); err != nil {
		return fmt.Errorf("pulumi export: %w", err)
	}
	for _, res := range exp.Deployment.Resources {
		if !res.Custom || strings.HasPrefix(res.Type, "pulumi:") {
			continue
		}
		stack, name := pulumiNames(res.URN)
		info := Info{Tool: "pulumi", Address: name, Stack: stack, Source: "state"}
		idx.add(res.ID, info)
		idx.add(cloud.LastPathSegment(res.ID), info)
	}
	return nil
}

func pulumiNames(urn string) (stack, name string) {
	// urn:pulumi:stack::project::type::name
	parts := strings.Split(urn, "::")
	if len(parts) >= 1 {
		head := strings.Split(parts[0], ":")
		if len(head) >= 3 {
			stack = head[2]
		}
	}
	if len(parts) >= 3 {
		name = parts[len(parts)-1]
	}
	return stack, name
}

// Snippet renders a plan-friendly change that an operator can paste into
// Terraform or Pulumi instead of mutating the live resource.
func Snippet(info Info, res cloud.Resource, action, spec string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CloudRipper refused to %s %s/%s (%s) because it is managed by %s",
		action, res.Provider, res.DisplayName(), res.ID, info.Tool)
	if info.Address != "" {
		fmt.Fprintf(&b, " (%s)", info.Address)
	}
	b.WriteString(".\nApply the change in the IaC project so state stays in sync.\n")
	if spec != "" {
		fmt.Fprintf(&b, "Proposed spec: %s\n", spec)
	}
	switch info.Tool {
	case "pulumi":
		b.WriteString("\n// Pulumi TypeScript\n")
		if spec != "" {
			fmt.Fprintf(&b, "// instanceType: %q,\n", spec)
		} else if action == "terminate" {
			b.WriteString("// remove this resource from the program\n")
		} else {
			fmt.Fprintf(&b, "// action: %s\n", action)
		}
	default:
		b.WriteString("\n# Terraform\n")
		if info.Address != "" && spec != "" {
			typeName, name, ok := splitAddress(info.Address)
			if ok {
				fmt.Fprintf(&b, "# resource %q %q {\n#   instance_type = %q\n# }\n", typeName, name, spec)
				return b.String()
			}
		}
		fmt.Fprintf(&b, "# action = %q\n", action)
		if spec != "" {
			fmt.Fprintf(&b, "# instance_type = %q\n", spec)
		}
	}
	return b.String()
}

func splitAddress(address string) (string, string, bool) {
	parts := strings.Split(address, ".")
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[len(parts)-2], parts[len(parts)-1], true
}

// WritePlan stores a snippet and returns the file path.
func WritePlan(dir string, res cloud.Resource, info Info, action, spec string) (string, error) {
	if dir == "" {
		dir = filepath.Join(".cloudripper", "plans")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := sanitize(res.Provider + "-" + res.ID + ".plan.txt")
	path := filepath.Join(dir, name)
	body := Snippet(info, res, action, spec)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func sanitize(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
