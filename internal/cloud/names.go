package cloud

import "strings"

// LastPathSegment returns the final slash-delimited piece of a URL or ID.
func LastPathSegment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "/")
	if value == "" {
		return ""
	}
	if i := strings.LastIndex(value, "/"); i >= 0 {
		return value[i+1:]
	}
	return value
}

// ResourceGroup extracts an Azure resource group from a resource ID.
func ResourceGroup(id string) string {
	parts := strings.Split(id, "/")
	for i, part := range parts {
		if strings.EqualFold(part, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// NormalizeCostID lowercases an ID and keeps the last path segment so billing
// rows (ARNs, full resource IDs) match scanner IDs.
func NormalizeCostID(id string) string {
	id = strings.TrimSpace(strings.ToLower(id))
	if id == "" {
		return ""
	}
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	return id
}

// SameResource reports whether a scanned ID and a billing ID refer to the same asset.
func SameResource(scannedID, billedID string) bool {
	a := NormalizeCostID(scannedID)
	b := NormalizeCostID(billedID)
	if a == "" || b == "" {
		return false
	}
	return a == b
}
