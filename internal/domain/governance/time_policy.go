package governance

import (
	"time"
)

// SimpleTimeRestriction defines simple time-based restrictions
type SimpleTimeRestriction struct {
	RestrictedDays  []time.Weekday
	RestrictedHours []int // 0-23
	Timezone        string
}

// DefaultSimpleTimeRestriction returns a policy restricting weekends and late nights (e.g. 10PM-6AM)
func DefaultSimpleTimeRestriction() SimpleTimeRestriction {
	return SimpleTimeRestriction{
		RestrictedDays:  []time.Weekday{time.Saturday, time.Sunday},
		RestrictedHours: []int{22, 23, 0, 1, 2, 3, 4, 5, 6},
		Timezone:        "UTC",
	}
}

// IsAllowed checks if an action is allowed at the given time
func (p *SimpleTimeRestriction) IsAllowed(t time.Time) bool {
	// Convert to policy timezone if needed (simplified here to assume UTC or local)

	// Check day
	for _, day := range p.RestrictedDays {
		if t.Weekday() == day {
			return false
		}
	}

	// Check hour
	hour := t.Hour()
	for _, h := range p.RestrictedHours {
		if hour == h {
			return false
		}
	}

	return true
}
