package cloud

import "fmt"

// UnsupportedError means the action is intentionally not implemented for the type.
type UnsupportedError struct {
	Action string
	Type   ResourceType
	Reason string
}

func (e *UnsupportedError) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return fmt.Sprintf("action %s is not supported for %s", e.Action, e.Type)
}
