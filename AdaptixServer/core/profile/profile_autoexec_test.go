package profile

import (
	"strings"
	"testing"
)

func TestIsValidAutoExecID(t *testing.T) {
	valid := []string{
		"artex-gopher",
		"notify.gopher",
		"persist_linux",
		"rule123",
		"a",
	}
	for _, id := range valid {
		if !isValidAutoExecID(id) {
			t.Fatalf("expected valid id %q", id)
		}
	}

	invalid := []string{
		"",
		"has space",
		"has/slash",
		"has:colon",
		"中文",
		strings.Repeat("a", 129),
	}
	for _, id := range invalid {
		if isValidAutoExecID(id) {
			t.Fatalf("expected invalid id %q", id)
		}
	}
}

func TestValidateAutoExecRulesAcceptsValidIDs(t *testing.T) {
	messages := validateAutoExecRules("on_agent_new", []TsAutoExecRule{
		{ID: "artex-gopher"},
		{ID: "notify-gopher"},
	})
	if len(messages) != 0 {
		t.Fatalf("unexpected messages: %v", messages)
	}
}

func TestValidateAutoExecRulesRejectsMissingID(t *testing.T) {
	messages := validateAutoExecRules("on_agent_new", []TsAutoExecRule{
		{ID: ""},
	})
	if len(messages) != 1 {
		t.Fatalf("messages = %v", messages)
	}
	if !strings.Contains(messages[0], "must be set") {
		t.Fatalf("message = %q", messages[0])
	}
}

func TestValidateAutoExecRulesRejectsInvalidCharacters(t *testing.T) {
	messages := validateAutoExecRules("on_agent_new", []TsAutoExecRule{
		{ID: "bad id"},
	})
	if len(messages) != 1 {
		t.Fatalf("messages = %v", messages)
	}
	if !strings.Contains(messages[0], "invalid characters") {
		t.Fatalf("message = %q", messages[0])
	}
}

func TestValidateAutoExecRulesRejectsDuplicateID(t *testing.T) {
	messages := validateAutoExecRules("on_agent_new", []TsAutoExecRule{
		{ID: "same"},
		{ID: "same"},
	})
	if len(messages) != 1 {
		t.Fatalf("messages = %v", messages)
	}
	if !strings.Contains(messages[0], "duplicates") {
		t.Fatalf("message = %q", messages[0])
	}
}

func TestValidateAutoExecRulesReportsEachInvalidRule(t *testing.T) {
	messages := validateAutoExecRules("on_agent_new", []TsAutoExecRule{
		{ID: ""},
		{ID: "ok"},
		{ID: "bad id"},
		{ID: "ok"},
	})
	if len(messages) != 3 {
		t.Fatalf("messages = %v", messages)
	}
}

func TestNormalizeTriggerTiming(t *testing.T) {
	if got := NormalizeTriggerTiming(""); got != DefaultTriggerTiming {
		t.Fatalf("empty normalizes to %q, want %q", got, DefaultTriggerTiming)
	}
	if got := NormalizeTriggerTiming("  "); got != DefaultTriggerTiming {
		t.Fatalf("blank normalizes to %q", got)
	}
	if got := NormalizeTriggerTiming("agent.online"); got != TriggerAgentOnline {
		t.Fatalf("agent.online normalizes to %q", got)
	}
	if got := NormalizeTriggerTiming("nope"); got != DefaultTriggerTiming {
		t.Fatalf("unknown normalizes to %q", got)
	}
}

func TestValidateAutoExecRulesRejectsUnknownTrigger(t *testing.T) {
	messages := validateAutoExecRules("rules", []TsAutoExecRule{
		{ID: "rule-a", Trigger: "nope"},
	})
	if len(messages) != 1 {
		t.Fatalf("messages = %v", messages)
	}
	if !strings.Contains(messages[0], "trigger") {
		t.Fatalf("message = %q", messages[0])
	}
}
