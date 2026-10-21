package cloud

import "strings"

// ProtectTag is the tag/label that keeps a resource out of destructive actions.
const ProtectTag = "cloudripper:protect"

// IsProtected reports whether tags mark a resource as off-limits.
// Accepted values are true, yes, y, and 1 (any case).
func IsProtected(tags map[string]string) bool {
	for k, v := range tags {
		if !strings.EqualFold(strings.TrimSpace(k), ProtectTag) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "y":
			return true
		}
	}
	return false
}
