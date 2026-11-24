package profile

import "strings"

// TriggerTiming identifies when an automation rule fires. It is deliberately
// independent of the eventing package: that package's asynchronous dispatcher
// drops hooks when its queue is full, which would silently skip an automation
// rule, and its base-event coverage has gaps that break cancellation.
type TriggerTiming string

const (
	// TriggerAgentOnline fires when a new agent checks in.
	TriggerAgentOnline TriggerTiming = "agent.online"

	// TriggerTaskComplete fires when a task finishes. Reserved for now.
	TriggerTaskComplete TriggerTiming = "task.complete"
)

// DefaultTriggerTiming is used when a rule does not declare one.
const DefaultTriggerTiming = TriggerAgentOnline

// AllTriggerTimings lists every timing an operator can pick.
var AllTriggerTimings = []TriggerTiming{
	TriggerAgentOnline,
	TriggerTaskComplete,
}

// NormalizeTriggerTiming maps an empty or unknown value to the default. An
// empty value must resolve to the default, otherwise legacy rules (which have
// no trigger field) would never match a firing timing.
func NormalizeTriggerTiming(value string) TriggerTiming {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return DefaultTriggerTiming
	}
	if TriggerTimingValid(trimmed) {
		return TriggerTiming(trimmed)
	}
	return DefaultTriggerTiming
}

// TriggerTimingValid reports whether the value names a known timing.
func TriggerTimingValid(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	for _, timing := range AllTriggerTimings {
		if string(timing) == value {
			return true
		}
	}
	return false
}

// TriggerTimingName returns a human readable label for the timing.
func TriggerTimingName(timing TriggerTiming) string {
	switch timing {
	case TriggerAgentOnline:
		return "Agent 上线"
	case TriggerTaskComplete:
		return "任务完成"
	default:
		return string(timing)
	}
}
