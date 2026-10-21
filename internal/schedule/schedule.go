package schedule

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// DefaultOffWindow stops unmanaged compute overnight UTC.
const DefaultOffWindow = "off=utc:19-07"

// Tag is the tag/label key that stores a schedule.
const Tag = "cloudripper:schedule"

var specPattern = regexp.MustCompile(`^off=utc:(\d{1,2})(?::(\d{2}))?-(\d{1,2})(?::(\d{2}))?$`)

// InOffWindow reports whether now falls inside an off window.
// The only supported form is off=utc:HH-HH or off=utc:HH:MM-HH:MM.
// A window that crosses midnight (19-07) is inclusive of the start and
// exclusive of the end.
func InOffWindow(spec string, now time.Time) (bool, error) {
	startMin, endMin, err := parseWindow(spec)
	if err != nil {
		return false, err
	}
	now = now.UTC()
	cur := now.Hour()*60 + now.Minute()
	if startMin == endMin {
		return false, fmt.Errorf("schedule %q has an empty window", spec)
	}
	if startMin < endMin {
		return cur >= startMin && cur < endMin, nil
	}
	return cur >= startMin || cur < endMin, nil
}

func parseWindow(spec string) (int, int, error) {
	m := specPattern.FindStringSubmatch(spec)
	if m == nil {
		return 0, 0, fmt.Errorf("unsupported schedule %q (want off=utc:19-07)", spec)
	}
	start, err := clock(m[1], m[2])
	if err != nil {
		return 0, 0, fmt.Errorf("schedule start: %w", err)
	}
	end, err := clock(m[3], m[4])
	if err != nil {
		return 0, 0, fmt.Errorf("schedule end: %w", err)
	}
	return start, end, nil
}

func clock(hour, minute string) (int, error) {
	h, err := strconv.Atoi(hour)
	if err != nil {
		return 0, err
	}
	min := 0
	if minute != "" {
		min, err = strconv.Atoi(minute)
		if err != nil {
			return 0, err
		}
	}
	if h < 0 || h > 23 || min < 0 || min > 59 {
		return 0, fmt.Errorf("clock %02d:%02d is out of range", h, min)
	}
	return h*60 + min, nil
}
