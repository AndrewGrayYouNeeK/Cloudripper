package cloud

import "strings"

// Size ladders are heuristics for one-step rightsizing. They are not a
// compatibility guarantee; apply shows the proposed size before changing it.
var sizeLadders = map[string][]string{
	"aws": {
		"t3.nano", "t3.micro", "t3.small", "t3.medium", "t3.large", "t3.xlarge",
		"m5.large", "m5.xlarge", "m5.2xlarge",
		"c5.xlarge", "c5.2xlarge", "c5.4xlarge",
	},
	"gcp": {
		"e2-micro", "e2-small", "e2-medium", "e2-standard-2", "e2-standard-4",
		"n1-standard-1", "n1-standard-2", "n1-standard-4",
	},
	"azure": {
		"Standard_B1s", "Standard_B2s", "Standard_D2s_v3", "Standard_D4s_v3", "Standard_D8s_v3",
	},
}

// CanonicalSpec strips URLs and surrounding space from an instance type.
func CanonicalSpec(spec string) string {
	spec = strings.TrimSpace(spec)
	if i := strings.LastIndex(spec, "/"); i >= 0 {
		spec = spec[i+1:]
	}
	return spec
}

// SmallerSpec returns the next cheaper size on the provider ladder.
func SmallerSpec(provider, spec string) (string, bool) {
	return stepSpec(provider, spec, -1)
}

// LargerSpec returns the next larger size on the provider ladder.
func LargerSpec(provider, spec string) (string, bool) {
	return stepSpec(provider, spec, 1)
}

func stepSpec(provider, spec string, delta int) (string, bool) {
	ladder, ok := sizeLadders[strings.ToLower(provider)]
	if !ok {
		return "", false
	}
	want := CanonicalSpec(spec)
	idx := -1
	for i, size := range ladder {
		if strings.EqualFold(size, want) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", false
	}
	next := idx + delta
	if next < 0 || next >= len(ladder) {
		return "", false
	}
	return ladder[next], true
}
