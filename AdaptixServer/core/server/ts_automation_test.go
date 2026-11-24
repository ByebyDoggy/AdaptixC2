package server

import (
	"AdaptixServer/core/profile"
	"AdaptixServer/core/utils/logs"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newAutomationTestTeamserver builds a Teamserver with an isolated data dir.
func newAutomationTestTeamserver(t *testing.T, rules []profile.TsAutoExecRule) (*Teamserver, string) {
	t.Helper()

	dir := t.TempDir()
	oldRepo := logs.RepoLogsInstance
	logs.RepoLogsInstance = &logs.RepoLogs{DataPath: dir}
	t.Cleanup(func() { logs.RepoLogsInstance = oldRepo })

	ts := &Teamserver{
		Profile: &profile.AdaptixProfile{
			AutoExec: &profile.TsAutoExec{OnAgentNew: rules},
		},
	}
	return ts, dir
}

func TestAutomationRuleEnabledDefaultsTrue(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if !ts.AutomationRuleEnabled("rule-a") {
		t.Fatal("rule without stored state must default to enabled")
	}
	if !ts.AutomationRuleEnabled("not-in-profile") {
		t.Fatal("unknown rule must default to enabled")
	}
}

func TestSetAutomationRuleEnabledPersists(t *testing.T) {
	ts, dir := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if err := ts.SetAutomationRuleEnabled("rule-a", false); err != nil {
		t.Fatalf("set: %v", err)
	}
	if ts.AutomationRuleEnabled("rule-a") {
		t.Fatal("rule should be disabled")
	}

	path := filepath.Join(dir, automationStoreFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var stored automationStore
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	state, exists := stored.Rules["rule-a"]
	if !exists {
		t.Fatal("rule state not persisted")
	}
	if state.isEnabled() {
		t.Fatal("persisted state should be disabled")
	}

	// A fresh load must keep the override.
	ts.automationStore = automationStore{}
	ts.loadAutomationStore()
	if ts.AutomationRuleEnabled("rule-a") {
		t.Fatal("disabled state lost after reload")
	}
}

func TestSetAutomationRuleEnabledRejectsUnknown(t *testing.T) {
	ts, dir := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if err := ts.SetAutomationRuleEnabled("missing", false); err == nil {
		t.Fatal("expected unknown rule to be rejected")
	}
	if _, err := os.Stat(filepath.Join(dir, automationStoreFileName)); !os.IsNotExist(err) {
		t.Fatalf("store file should not be written, err=%v", err)
	}
}

func TestAutomationRulesSnapshot(t *testing.T) {
	rules := []profile.TsAutoExecRule{
		{
			ID:          "artex-gopher",
			Name:        "Gopher recon",
			Description: "runs artex",
			Agent:       "gopher",
			OS:          "linux",
			Tag:         "auto",
			Commands:    []string{"shell id", "shell uname -a"},
			DelaySec:    3,
			Artex:       &profile.TsAutoExecArtex{URL: "http://127.0.0.1:8787"},
			Persist:     &profile.TsAutoExecPersist{Cron: true},
		},
		{
			ID: "notify-gopher",
			// no Name: must fall back to the id
			Agent:  "gopher",
			Notify: &profile.TsAutoExecNotify{},
		},
	}

	ts, _ := newAutomationTestTeamserver(t, rules)
	if err := ts.SetAutomationRuleEnabled("notify-gopher", false); err != nil {
		t.Fatalf("set: %v", err)
	}

	snapshot := ts.AutomationRulesSnapshot()
	if len(snapshot) != 2 {
		t.Fatalf("snapshot size = %d", len(snapshot))
	}

	first := snapshot[0]
	if first.ID != "artex-gopher" || !first.Enabled {
		t.Fatalf("first rule = %+v", first)
	}
	if first.Actions.Commands != 2 || !first.Actions.Artex || !first.Actions.Persist || first.Actions.Notify {
		t.Fatalf("actions = %+v", first.Actions)
	}
	if first.DelaySeconds != 3 {
		t.Fatalf("delay = %d", first.DelaySeconds)
	}

	second := snapshot[1]
	if second.Name != "notify-gopher" {
		t.Fatalf("name fallback = %q", second.Name)
	}
	if second.Enabled {
		t.Fatal("notify rule should be disabled")
	}
	if !second.Actions.Notify {
		t.Fatalf("notify action missing: %+v", second.Actions)
	}
}

func TestLoadAutomationStorePrunesUnknownRules(t *testing.T) {
	ts, dir := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "known"}})

	stored := newAutomationStore()
	stored.Rules["known"] = automationRuleState{}
	stored.Rules["removed"] = automationRuleState{}
	data, _ := json.Marshal(stored)
	if err := os.WriteFile(filepath.Join(dir, automationStoreFileName), data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	ts.loadAutomationStore()

	ts.automationMu.RLock()
	_, hasKnown := ts.automationStore.Rules["known"]
	_, hasRemoved := ts.automationStore.Rules["removed"]
	ts.automationMu.RUnlock()

	if !hasKnown {
		t.Fatal("known rule state was dropped")
	}
	if hasRemoved {
		t.Fatal("unknown rule state was not pruned")
	}
}

func TestRecordAutomationRun(t *testing.T) {
	ts, dir := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	ts.RecordAutomationRun("rule-a", "partial", "1/2 action(s) failed: boom")

	ts.automationMu.RLock()
	state := ts.automationStore.Rules["rule-a"]
	ts.automationMu.RUnlock()

	if state.LastRun <= 0 {
		t.Fatal("last_run not set")
	}
	if state.LastStatus != "partial" {
		t.Fatalf("last_status = %q", state.LastStatus)
	}
	if state.LastMessage == "" {
		t.Fatal("last_message not set")
	}
	if !state.isEnabled() {
		t.Fatal("recording a run must not disable the rule")
	}

	data, err := os.ReadFile(filepath.Join(dir, automationStoreFileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var stored automationStore
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stored.Rules["rule-a"].LastStatus != "partial" {
		t.Fatal("last_status not persisted")
	}
}

func TestAutomationServerChanConfigured(t *testing.T) {
	ts := &Teamserver{Profile: &profile.AdaptixProfile{AutoExec: &profile.TsAutoExec{}}}
	if ts.AutomationServerChanConfigured() {
		t.Fatal("empty config must not be reported as configured")
	}

	ts.Profile.AutoExec.ServerChan = &profile.TsServerChanConfig{SendKey: "SCTtest"}
	if !ts.AutomationServerChanConfigured() {
		t.Fatal("send key should be reported as configured")
	}
}

func TestSetAutomationRuleOrderPersists(t *testing.T) {
	ts, dir := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a", Order: 5}})

	if err := ts.SetAutomationRuleOrder("rule-a", 42); err != nil {
		t.Fatalf("set order: %v", err)
	}
	if got := ts.AutomationRuleOrder("rule-a"); got != 42 {
		t.Fatalf("order = %d, want 42", got)
	}

	data, err := os.ReadFile(filepath.Join(dir, automationStoreFileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var stored automationStore
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stored.Rules["rule-a"].Order == nil || *stored.Rules["rule-a"].Order != 42 {
		t.Fatalf("persisted order = %v", stored.Rules["rule-a"].Order)
	}

	// A reload keeps the override.
	ts.automationStore = automationStore{}
	ts.loadAutomationStore()
	if got := ts.AutomationRuleOrder("rule-a"); got != 42 {
		t.Fatalf("order after reload = %d, want 42", got)
	}
}

func TestAutomationRuleOrderFallsBackToProfile(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a", Order: 7}})

	if got := ts.AutomationRuleOrder("rule-a"); got != 7 {
		t.Fatalf("order = %d, want 7 (profile default)", got)
	}
}

func TestSetAutomationRuleOrderRejectsUnknown(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if err := ts.SetAutomationRuleOrder("missing", 1); err == nil {
		t.Fatal("expected unknown rule to be rejected")
	}
}

func TestAutomationTemplateSetAndGet(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if _, err := ts.TsAutomationTemplateGet("artex", "recon"); err == nil {
		t.Fatal("expected unknown template error")
	}

	view := profile.AutomationTemplateView{Name: "Recon", Goal: "custom goal for {{AGENT_HOST}}"}
	if err := ts.TsAutomationTemplateSet("artex", "recon", view); err != nil {
		t.Fatalf("set template: %v", err)
	}

	got, err := ts.TsAutomationTemplateGet("artex", "recon")
	if err != nil {
		t.Fatalf("get template: %v", err)
	}
	if got.Goal != view.Goal {
		t.Fatalf("goal = %q", got.Goal)
	}
	if got.Name != "Recon" {
		t.Fatalf("name = %q", got.Name)
	}
}

func TestAutomationTemplateSetRequiresContent(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if err := ts.TsAutomationTemplateSet("artex", "empty", profile.AutomationTemplateView{}); err == nil {
		t.Fatal("expected artex template without goal to be rejected")
	}
	if err := ts.TsAutomationTemplateSet("notify", "empty", profile.AutomationTemplateView{}); err == nil {
		t.Fatal("expected notify template without content to be rejected")
	}
}

func TestAutomationTemplateDeleteRefusesWhenReferenced(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	view := profile.AutomationTemplateView{Name: "Recon", Goal: "goal"}
	if err := ts.TsAutomationTemplateSet("artex", "recon", view); err != nil {
		t.Fatalf("set template: %v", err)
	}
	if err := ts.AutomationRuleUpdate("rule-a", map[string]any{"artex_template_id": "recon"}); err != nil {
		t.Fatalf("attach template: %v", err)
	}

	err := ts.TsAutomationTemplateDelete("artex", "recon")
	if err == nil {
		t.Fatal("expected delete of a referenced template to be refused")
	}
	if !strings.Contains(err.Error(), "rule-a") {
		t.Fatalf("error should name the referencing rule, got: %v", err)
	}

	// Detaching the rule makes the template deletable.
	if err := ts.AutomationRuleUpdate("rule-a", map[string]any{"artex_template_id": ""}); err != nil {
		t.Fatalf("detach template: %v", err)
	}
	if err := ts.TsAutomationTemplateDelete("artex", "recon"); err != nil {
		t.Fatalf("delete template: %v", err)
	}
}

func TestAutomationRuleUpdateRejectsUnknownTemplate(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if err := ts.AutomationRuleUpdate("rule-a", map[string]any{"artex_template_id": "nope"}); err == nil {
		t.Fatal("expected unknown artex template to be rejected")
	}
	if err := ts.AutomationRuleUpdate("rule-a", map[string]any{"notify_template_id": "nope"}); err == nil {
		t.Fatal("expected unknown notify template to be rejected")
	}
}

func TestAutomationRuleUpdateRejectsUnknownChannel(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if err := ts.AutomationRuleUpdate("rule-a", map[string]any{"notify_channel_id": "nope"}); err == nil {
		t.Fatal("expected unknown channel to be rejected")
	}
}

func TestAutomationRuleUpdateTrigger(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if err := ts.AutomationRuleUpdate("rule-a", map[string]any{"trigger": "agent.online"}); err != nil {
		t.Fatalf("set trigger: %v", err)
	}
	if got := ts.AutomationRuleTrigger("rule-a"); got != profile.TriggerAgentOnline {
		t.Fatalf("trigger = %q", got)
	}
	if err := ts.AutomationRuleUpdate("rule-a", map[string]any{"trigger": "nope"}); err == nil {
		t.Fatal("expected unknown trigger to be rejected")
	}
}

func TestAutomationV1Migration(t *testing.T) {
	ts, dir := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	legacy := `{"version":1,"rules":{},"artex_templates":{"rule-a":"legacy goal {{AGENT_HOST}}"},"serverchan":{"send_key":"SCTold"}}`
	if err := os.WriteFile(filepath.Join(dir, automationStoreFileName), []byte(legacy), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	ts.loadAutomationStore()

	if got := ts.AutomationRuleArtexTemplateID("rule-a"); got != "legacy-rule-a" {
		t.Fatalf("migrated template id = %q", got)
	}
	tpl, err := ts.TsAutomationTemplateGet("artex", "legacy-rule-a")
	if err != nil {
		t.Fatalf("migrated template missing: %v", err)
	}
	if tpl.Goal != "legacy goal {{AGENT_HOST}}" {
		t.Fatalf("migrated goal = %q", tpl.Goal)
	}
	if got := ts.AutomationServerChanSendKey(); got != "SCTold" {
		t.Fatalf("migrated send key = %q", got)
	}
}

func TestCloneAutomationStoreKeepsNewFields(t *testing.T) {
	enabled := false
	order := 42
	src := newAutomationStore()
	src.Rules["rule-a"] = automationRuleState{Enabled: &enabled, Order: &order, CommandsSet: true, Commands: []string{"shell id"}}
	src.Templates.Artex["t1"] = automationTemplate{Name: "T1", Goal: "goal"}
	src.Templates.Notify["n1"] = automationTemplate{Name: "N1", Title: "title"}
	src.Channels["serverchan"] = automationChannelState{SendKey: "SCTx"}

	dst := cloneAutomationStore(src)

	state := dst.Rules["rule-a"]
	if state.Enabled == nil || *state.Enabled {
		t.Fatal("enabled override lost")
	}
	if state.Order == nil || *state.Order != 42 {
		t.Fatal("order override lost")
	}
	if !state.CommandsSet || len(state.Commands) != 1 {
		t.Fatal("commands override lost")
	}
	if dst.Templates.Artex["t1"].Goal != "goal" {
		t.Fatal("artex template lost")
	}
	if dst.Templates.Notify["n1"].Title != "title" {
		t.Fatal("notify template lost")
	}
	if dst.Channels["serverchan"].SendKey != "SCTx" {
		t.Fatal("channel credential lost")
	}
}

func TestAutomationServerChanSet(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if ts.AutomationServerChanConfigured() {
		t.Fatal("must not be configured initially")
	}

	if err := ts.TsAutomationServerChanSet("SCTnewkey123456", "http://127.0.0.1:18080"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if !ts.AutomationServerChanConfigured() {
		t.Fatal("should be configured after set")
	}
	if got := ts.AutomationServerChanSendKey(); got != "SCTnewkey123456" {
		t.Fatalf("send key = %q", got)
	}
	if got := ts.AutomationServerChanAPIBase(); got != "http://127.0.0.1:18080" {
		t.Fatalf("api base = %q", got)
	}

	// An empty key keeps the current value.
	if err := ts.TsAutomationServerChanSet("", ""); err != nil {
		t.Fatalf("set empty: %v", err)
	}
	if got := ts.AutomationServerChanSendKey(); got != "SCTnewkey123456" {
		t.Fatalf("send key changed to %q", got)
	}
}

func TestMaskServerChanKey(t *testing.T) {
	if got := maskServerChanKey(""); got != "" {
		t.Fatalf("mask of empty = %q", got)
	}
	if got := maskServerChanKey("SCT428286TSTjgCuVK99P276JPHwimCfQR"); got != "SCT4...CfQR" {
		t.Fatalf("mask = %q", got)
	}
	if got := maskServerChanKey("short"); got != "s...t" {
		t.Fatalf("mask of short = %q", got)
	}
}

func TestAutomationServerChanFallsBackToProfile(t *testing.T) {
	ts := &Teamserver{Profile: &profile.AdaptixProfile{
		AutoExec: &profile.TsAutoExec{
			ServerChan: &profile.TsServerChanConfig{SendKey: "SCTprofile"},
		},
	}}

	if got := ts.AutomationServerChanSendKey(); got != "SCTprofile" {
		t.Fatalf("send key = %q", got)
	}
	if !ts.AutomationServerChanConfigured() {
		t.Fatal("profile default should count as configured")
	}
}
