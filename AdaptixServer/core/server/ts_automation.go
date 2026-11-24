package server

import (
	"AdaptixServer/core/profile"
	"AdaptixServer/core/utils/logs"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	automationStoreFileName = "automation.json"
	automationStoreVersion  = 2

	templateKindArtex  = "artex"
	templateKindNotify = "notify"

	legacyTemplatePrefix = "legacy-"
)

// automationRuleState is the persisted per-rule state. Every field except the
// last-run trio is an optional override: a nil pointer means "use the value
// from profile.yaml".
type automationRuleState struct {
	Enabled          *bool    `json:"enabled,omitempty"`
	Order            *int     `json:"order,omitempty"`
	Trigger          string   `json:"trigger,omitempty"`
	Name             string   `json:"name,omitempty"`
	Description      string   `json:"description,omitempty"`
	DelaySeconds     *int     `json:"delay_seconds,omitempty"`
	Commands         []string `json:"commands,omitempty"`
	CommandsSet      bool     `json:"commands_set,omitempty"`
	ArtexTemplateID  string   `json:"artex_template_id,omitempty"`
	NotifyChannelID  string   `json:"notify_channel_id,omitempty"`
	NotifyTemplateID string   `json:"notify_template_id,omitempty"`
	LastRun          int64    `json:"last_run"`
	LastStatus       string   `json:"last_status"`
	LastMessage      string   `json:"last_message"`
}

func (s automationRuleState) isEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// automationTemplate is a global named template. ARTEX templates fill Goal and
// the task parameters; notification templates fill Title and Body.
type automationTemplate struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	Goal            string `json:"goal,omitempty"`
	Title           string `json:"title,omitempty"`
	Body            string `json:"body,omitempty"`
	WorkerCount     int    `json:"worker_count,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
	SeedFirstIntent bool   `json:"seed_first_intent,omitempty"`
	DedupBy         string `json:"dedup_by,omitempty"`
	CooldownSeconds int    `json:"cooldown_seconds,omitempty"`
	SkipIfRunning   bool   `json:"skip_if_running,omitempty"`
	UpdatedAt       int64  `json:"updated_at"`
}

// automationChannelState holds one channel's credential. The teamserver is the
// only holder of the plain secret; clients only ever see a mask.
type automationChannelState struct {
	SendKey string `json:"send_key"`
	APIBase string `json:"api_base"`
}

type automationTemplates struct {
	Artex  map[string]automationTemplate `json:"artex"`
	Notify map[string]automationTemplate `json:"notify"`
}

type automationStore struct {
	Version   int                               `json:"version"`
	Rules     map[string]automationRuleState    `json:"rules"`
	Templates automationTemplates               `json:"templates"`
	Channels  map[string]automationChannelState `json:"channels"`
}

// automationStoreFile mirrors the on-disk document and additionally carries the
// v1 fields so an upgrade can migrate them in place.
type automationStoreFile struct {
	Version   int                               `json:"version"`
	Rules     map[string]automationRuleState    `json:"rules"`
	Templates *automationTemplates              `json:"templates"`
	Channels  map[string]automationChannelState `json:"channels"`

	// v1 layout, read-only during migration.
	ArtexTemplates map[string]string       `json:"artex_templates,omitempty"`
	ServerChan     *automationChannelState `json:"serverchan,omitempty"`
}

func automationStorePath() string {
	return filepath.Join(logs.RepoLogsInstance.DataPath, automationStoreFileName)
}

func newAutomationStore() automationStore {
	return automationStore{
		Version: automationStoreVersion,
		Rules:   make(map[string]automationRuleState),
		Templates: automationTemplates{
			Artex:  make(map[string]automationTemplate),
			Notify: make(map[string]automationTemplate),
		},
		Channels: make(map[string]automationChannelState),
	}
}

func cloneAutomationStore(src automationStore) automationStore {
	dst := newAutomationStore()
	dst.Version = src.Version

	for id, state := range src.Rules {
		cloned := state
		if state.CommandsSet {
			cloned.Commands = append([]string(nil), state.Commands...)
		}
		dst.Rules[id] = cloned
	}
	for id, tpl := range src.Templates.Artex {
		dst.Templates.Artex[id] = tpl
	}
	for id, tpl := range src.Templates.Notify {
		dst.Templates.Notify[id] = tpl
	}
	for id, ch := range src.Channels {
		dst.Channels[id] = ch
	}
	return dst
}

// loadAutomationStore loads data/automation.json, migrates a v1 document and
// drops entries whose rule no longer exists in the profile.
func (ts *Teamserver) loadAutomationStore() {
	store := newAutomationStore()
	path := automationStorePath()

	data, err := os.ReadFile(path)
	if err == nil {
		var file automationStoreFile
		if err := json.Unmarshal(data, &file); err != nil {
			logs.Warn("", "Failed to parse %s: %s", path, err.Error())
		} else {
			store = migrateAutomationFile(file)
		}
	} else if !os.IsNotExist(err) {
		logs.Warn("", "Failed to read %s: %s", path, err.Error())
	}

	known := ts.autoExecRulesByID()
	for id := range store.Rules {
		if _, exists := known[id]; !exists {
			logs.Warn("", "Automation: dropping state for unknown rule '%s'", id)
			delete(store.Rules, id)
		}
	}

	ts.automationMu.Lock()
	ts.automationStore = store
	ts.automationMu.Unlock()
}

// migrateAutomationFile turns any supported on-disk layout into the current
// in-memory store. A v1 document keeps working unchanged: its per-rule ARTEX
// template text becomes a `legacy-<ruleID>` template that the rule references.
func migrateAutomationFile(file automationStoreFile) automationStore {
	store := newAutomationStore()

	if file.Rules != nil {
		store.Rules = file.Rules
	}
	if file.Version > 0 {
		store.Version = file.Version
	}
	if file.Templates != nil {
		if file.Templates.Artex != nil {
			store.Templates.Artex = file.Templates.Artex
		}
		if file.Templates.Notify != nil {
			store.Templates.Notify = file.Templates.Notify
		}
	}
	for id, channel := range file.Channels {
		store.Channels[id] = channel
	}

	// v1: per-rule ARTEX template text -> named template + rule reference.
	for ruleID, goal := range file.ArtexTemplates {
		if strings.TrimSpace(goal) == "" {
			continue
		}
		templateID := legacyTemplatePrefix + ruleID
		if _, exists := store.Templates.Artex[templateID]; !exists {
			store.Templates.Artex[templateID] = automationTemplate{
				Name:      "Legacy " + ruleID,
				Goal:      goal,
				UpdatedAt: time.Now().Unix(),
			}
			logs.Info("", "Automation: migrated v1 template for '%s' to template '%s'", ruleID, templateID)
		}
		state := store.Rules[ruleID]
		if state.ArtexTemplateID == "" {
			state.ArtexTemplateID = templateID
			store.Rules[ruleID] = state
		}
	}

	// v1: single ServerChan block -> channel entry.
	if file.ServerChan != nil {
		if _, exists := store.Channels[NotifyChannelServerChan]; !exists &&
			(file.ServerChan.SendKey != "" || file.ServerChan.APIBase != "") {
			store.Channels[NotifyChannelServerChan] = *file.ServerChan
			logs.Info("", "Automation: migrated v1 serverchan credential into the channel store")
		}
	}

	if store.Version < automationStoreVersion {
		store.Version = automationStoreVersion
	}
	return store
}

// autoExecRules returns every configured rule, normalising the legacy
// on_agent_new list into the same shape as the rules list.
func (ts *Teamserver) autoExecRules() []profile.TsAutoExecRule {
	if ts.Profile == nil || ts.Profile.AutoExec == nil {
		return nil
	}

	merged := make(map[string]profile.TsAutoExecRule)
	order := make([]string, 0, len(ts.Profile.AutoExec.OnAgentNew)+len(ts.Profile.AutoExec.Rules))

	for _, rule := range ts.Profile.AutoExec.OnAgentNew {
		rule.Trigger = string(profile.NormalizeTriggerTiming(rule.Trigger))
		if _, exists := merged[rule.ID]; !exists {
			order = append(order, rule.ID)
		}
		merged[rule.ID] = rule
	}
	// Rules wins over OnAgentNew when both declare the same id.
	for _, rule := range ts.Profile.AutoExec.Rules {
		rule.Trigger = string(profile.NormalizeTriggerTiming(rule.Trigger))
		if _, exists := merged[rule.ID]; !exists {
			order = append(order, rule.ID)
		}
		merged[rule.ID] = rule
	}

	result := make([]profile.TsAutoExecRule, 0, len(order))
	for _, id := range order {
		result = append(result, merged[id])
	}
	return result
}

// autoExecRulesByID indexes the profile rules by their stable id.
func (ts *Teamserver) autoExecRulesByID() map[string]profile.TsAutoExecRule {
	result := make(map[string]profile.TsAutoExecRule)
	for _, rule := range ts.autoExecRules() {
		result[rule.ID] = rule
	}
	return result
}

// AutomationRuleEnabled reports whether a rule may run. Unknown rules are
// treated as enabled so a profile edit cannot silently disable everything.
func (ts *Teamserver) AutomationRuleEnabled(id string) bool {
	ts.automationMu.RLock()
	state, exists := ts.automationStore.Rules[id]
	ts.automationMu.RUnlock()

	if !exists {
		return true
	}
	return state.isEnabled()
}

// SetAutomationRuleEnabled persists a toggle override. It writes to disk
// before updating memory, mirroring TsChainConfigSet.
func (ts *Teamserver) SetAutomationRuleEnabled(id string, enabled bool) error {
	if id == "" {
		return errors.New("rule id is required")
	}
	if _, exists := ts.autoExecRulesByID()[id]; !exists {
		return fmt.Errorf("unknown rule '%s'", id)
	}

	ts.automationMu.Lock()
	defer ts.automationMu.Unlock()

	next := cloneAutomationStore(ts.automationStore)
	state := next.Rules[id]
	state.Enabled = &enabled
	next.Rules[id] = state

	if err := saveAutomationStore(next); err != nil {
		return err
	}
	ts.automationStore = next

	logs.Info("", "Automation: rule '%s' enabled=%v", id, enabled)
	return nil
}

// SetAutomationRuleOrder persists an execution order override.
func (ts *Teamserver) SetAutomationRuleOrder(id string, order int) error {
	if id == "" {
		return errors.New("rule id is required")
	}
	if _, exists := ts.autoExecRulesByID()[id]; !exists {
		return fmt.Errorf("unknown rule '%s'", id)
	}

	ts.automationMu.Lock()
	defer ts.automationMu.Unlock()

	next := cloneAutomationStore(ts.automationStore)
	state := next.Rules[id]
	state.Order = &order
	next.Rules[id] = state

	if err := saveAutomationStore(next); err != nil {
		return err
	}
	ts.automationStore = next

	logs.Info("", "Automation: rule '%s' order=%d", id, order)
	return nil
}

// AutomationRuleUpdate applies a partial rule configuration. Recognised keys:
// enabled, order, trigger, name, description, delay_seconds, commands,
// artex_template_id, notify_channel_id, notify_template_id.
//
// Template and channel references are validated against the template store so a
// rule can never point at something that does not exist.
func (ts *Teamserver) AutomationRuleUpdate(ruleID string, patch map[string]any) error {
	if ruleID == "" {
		return errors.New("rule id is required")
	}
	if _, exists := ts.autoExecRulesByID()[ruleID]; !exists {
		return fmt.Errorf("unknown rule '%s'", ruleID)
	}

	// Channel existence is checked before the write lock: the registry accessor
	// takes the same mutex, so calling it while locked would deadlock.
	if raw, exists := patch["notify_channel_id"]; exists {
		value, ok := raw.(string)
		if !ok {
			return fmt.Errorf("'notify_channel_id' must be a string")
		}
		if strings.TrimSpace(value) != "" {
			if _, exists := ts.NotifyChannel(strings.TrimSpace(value)); !exists {
				return fmt.Errorf("unknown notification channel '%s'", strings.TrimSpace(value))
			}
		}
	}

	ts.automationMu.Lock()
	defer ts.automationMu.Unlock()

	next := cloneAutomationStore(ts.automationStore)
	state := next.Rules[ruleID]

	for key, raw := range patch {
		switch key {
		case "enabled":
			value, ok := raw.(bool)
			if !ok {
				return fmt.Errorf("'enabled' must be a boolean")
			}
			state.Enabled = &value

		case "order":
			value, err := toInt(raw)
			if err != nil {
				return fmt.Errorf("'order': %w", err)
			}
			state.Order = &value

		case "delay_seconds":
			value, err := toInt(raw)
			if err != nil {
				return fmt.Errorf("'delay_seconds': %w", err)
			}
			if value < 0 {
				value = 0
			}
			state.DelaySeconds = &value

		case "trigger":
			value, ok := raw.(string)
			if !ok {
				return fmt.Errorf("'trigger' must be a string")
			}
			if !profile.TriggerTimingValid(value) {
				return fmt.Errorf("unknown trigger timing '%s'", value)
			}
			state.Trigger = string(profile.NormalizeTriggerTiming(value))

		case "name":
			value, ok := raw.(string)
			if !ok {
				return fmt.Errorf("'name' must be a string")
			}
			state.Name = strings.TrimSpace(value)

		case "description":
			value, ok := raw.(string)
			if !ok {
				return fmt.Errorf("'description' must be a string")
			}
			state.Description = strings.TrimSpace(value)

		case "commands":
			lines, err := toStringSlice(raw)
			if err != nil {
				return fmt.Errorf("'commands': %w", err)
			}
			state.Commands = lines
			state.CommandsSet = true

		case "artex_template_id":
			value, ok := raw.(string)
			if !ok {
				return fmt.Errorf("'artex_template_id' must be a string")
			}
			value = strings.TrimSpace(value)
			if value != "" {
				if _, exists := next.Templates.Artex[value]; !exists {
					return fmt.Errorf("unknown artex template '%s'", value)
				}
			}
			state.ArtexTemplateID = value

		case "notify_channel_id":
			value, ok := raw.(string)
			if !ok {
				return fmt.Errorf("'notify_channel_id' must be a string")
			}
			// Already validated above, before the write lock was taken.
			state.NotifyChannelID = strings.TrimSpace(value)

		case "notify_template_id":
			value, ok := raw.(string)
			if !ok {
				return fmt.Errorf("'notify_template_id' must be a string")
			}
			value = strings.TrimSpace(value)
			if value != "" {
				if _, exists := next.Templates.Notify[value]; !exists {
					return fmt.Errorf("unknown notification template '%s'", value)
				}
			}
			state.NotifyTemplateID = value

		default:
			return fmt.Errorf("unknown field '%s'", key)
		}
	}

	next.Rules[ruleID] = state

	if err := saveAutomationStore(next); err != nil {
		return err
	}
	ts.automationStore = next

	logs.Info("", "Automation: rule '%s' configuration updated (%d field(s))", ruleID, len(patch))
	return nil
}

// AutomationRuleOrder returns the effective order of a rule: the runtime
// override when set, otherwise the profile value.
func (ts *Teamserver) AutomationRuleOrder(id string) int {
	ts.automationMu.RLock()
	state, exists := ts.automationStore.Rules[id]
	ts.automationMu.RUnlock()

	if exists && state.Order != nil {
		return *state.Order
	}
	return ts.autoExecRulesByID()[id].Order
}

// AutomationRuleTrigger returns the effective trigger timing of a rule.
func (ts *Teamserver) AutomationRuleTrigger(id string) profile.TriggerTiming {
	ts.automationMu.RLock()
	state := ts.automationStore.Rules[id]
	ts.automationMu.RUnlock()

	if state.Trigger != "" {
		return profile.NormalizeTriggerTiming(state.Trigger)
	}
	return profile.NormalizeTriggerTiming(ts.autoExecRulesByID()[id].Trigger)
}

// AutomationRuleArtexTemplateID returns the effective ARTEX template reference.
func (ts *Teamserver) AutomationRuleArtexTemplateID(id string) string {
	ts.automationMu.RLock()
	state := ts.automationStore.Rules[id]
	ts.automationMu.RUnlock()

	if state.ArtexTemplateID != "" {
		return state.ArtexTemplateID
	}
	rule := ts.autoExecRulesByID()[id]
	if rule.Artex != nil {
		return rule.Artex.TemplateID
	}
	return ""
}

// automationEffectiveRule applies the runtime overrides to a profile rule so
// callers execute exactly what the operator configured in the client.
func (ts *Teamserver) automationEffectiveRule(rule profile.TsAutoExecRule) profile.TsAutoExecRule {
	ts.automationMu.RLock()
	state := ts.automationStore.Rules[rule.ID]
	ts.automationMu.RUnlock()

	if state.CommandsSet {
		rule.Commands = state.Commands
	}
	if state.DelaySeconds != nil {
		rule.DelaySec = *state.DelaySeconds
	}
	return rule
}

// RecordAutomationRun stores the last trigger outcome for a rule.
func (ts *Teamserver) RecordAutomationRun(id, status, message string) {
	if id == "" {
		return
	}

	ts.automationMu.Lock()
	defer ts.automationMu.Unlock()

	next := cloneAutomationStore(ts.automationStore)
	state := next.Rules[id]
	state.LastRun = time.Now().Unix()
	state.LastStatus = status
	state.LastMessage = message
	next.Rules[id] = state

	if err := saveAutomationStore(next); err != nil {
		logs.Warn("", "Automation: failed to record run for '%s': %s", id, err.Error())
		return
	}
	ts.automationStore = next
}

// AutomationRulesSnapshot merges profile definitions with persisted overrides.
func (ts *Teamserver) AutomationRulesSnapshot() []profile.AutomationRuleView {
	result := []profile.AutomationRuleView{}

	rules := ts.autoExecRules()
	if len(rules) == 0 {
		return result
	}

	ts.automationMu.RLock()
	states := make(map[string]automationRuleState, len(ts.automationStore.Rules))
	for id, state := range ts.automationStore.Rules {
		states[id] = state
	}
	ts.automationMu.RUnlock()

	for _, rule := range rules {
		state := states[rule.ID]

		name := rule.Name
		if state.Name != "" {
			name = state.Name
		}
		if name == "" {
			name = rule.ID
		}

		description := rule.Description
		if state.Description != "" {
			description = state.Description
		}

		order := rule.Order
		if state.Order != nil {
			order = *state.Order
		}

		trigger := string(profile.NormalizeTriggerTiming(rule.Trigger))
		if state.Trigger != "" {
			trigger = state.Trigger
		}

		delay := rule.DelaySec
		if state.DelaySeconds != nil {
			delay = *state.DelaySeconds
		}

		commands := rule.Commands
		if state.CommandsSet {
			commands = state.Commands
		}

		artexTemplateID := ""
		if rule.Artex != nil {
			artexTemplateID = rule.Artex.TemplateID
		}
		if state.ArtexTemplateID != "" {
			artexTemplateID = state.ArtexTemplateID
		}

		notifyChannelID := ""
		notifyTemplateID := ""
		if rule.Notify != nil {
			notifyChannelID = rule.Notify.ChannelID
			notifyTemplateID = rule.Notify.TemplateID
		}
		if state.NotifyChannelID != "" {
			notifyChannelID = state.NotifyChannelID
		}
		if state.NotifyTemplateID != "" {
			notifyTemplateID = state.NotifyTemplateID
		}
		if notifyChannelID == "" && rule.Notify != nil {
			notifyChannelID = NotifyChannelServerChan
		}

		result = append(result, profile.AutomationRuleView{
			ID:               rule.ID,
			Name:             name,
			Description:      description,
			Order:            order,
			Trigger:          trigger,
			TriggerName:      profile.TriggerTimingName(profile.TriggerTiming(trigger)),
			Agent:            rule.Agent,
			Listener:         rule.Listener,
			OS:               rule.OS,
			Tag:              rule.Tag,
			DelaySeconds:     delay,
			Enabled:          state.isEnabled(),
			Commands:         append([]string(nil), commands...),
			ArtexTemplateID:  artexTemplateID,
			NotifyChannelID:  notifyChannelID,
			NotifyTemplateID: notifyTemplateID,
			Script:           rule.Script,
			ScriptArgs:       append([]string(nil), rule.ScriptArgs...),
			Actions: profile.AutomationActions{
				Commands:      len(commands),
				Persist:       rule.Persist != nil,
				Artex:         rule.Artex != nil || rule.Script != "",
				ArtexTemplate: artexTemplateID,
				Notify:        rule.Notify != nil,
				NotifyChannel: notifyChannelID,
				Script:        rule.Script,
			},
			LastRun:     state.LastRun,
			LastStatus:  state.LastStatus,
			LastMessage: state.LastMessage,
		})
	}

	return result
}

// --- templates ---------------------------------------------------------------

func validTemplateKind(kind string) bool {
	return kind == templateKindArtex || kind == templateKindNotify
}

// TsAutomationTemplatesList returns every template of one kind, sorted by id.
func (ts *Teamserver) TsAutomationTemplatesList(kind string) []profile.AutomationTemplateView {
	result := []profile.AutomationTemplateView{}
	if !validTemplateKind(kind) {
		return result
	}

	ts.automationMu.RLock()
	source := ts.automationStore.Templates.Artex
	if kind == templateKindNotify {
		source = ts.automationStore.Templates.Notify
	}
	for id, tpl := range source {
		result = append(result, automationTemplateView(kind, id, tpl))
	}
	ts.automationMu.RUnlock()

	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// TsAutomationTemplateGet returns one template.
func (ts *Teamserver) TsAutomationTemplateGet(kind, id string) (profile.AutomationTemplateView, error) {
	if !validTemplateKind(kind) {
		return profile.AutomationTemplateView{}, fmt.Errorf("unknown template kind '%s'", kind)
	}

	ts.automationMu.RLock()
	defer ts.automationMu.RUnlock()

	source := ts.automationStore.Templates.Artex
	if kind == templateKindNotify {
		source = ts.automationStore.Templates.Notify
	}
	tpl, exists := source[id]
	if !exists {
		return profile.AutomationTemplateView{}, fmt.Errorf("unknown %s template '%s'", kind, id)
	}
	return automationTemplateView(kind, id, tpl), nil
}

// TsAutomationTemplateSet creates or replaces a template.
func (ts *Teamserver) TsAutomationTemplateSet(kind, id string, view profile.AutomationTemplateView) error {
	if !validTemplateKind(kind) {
		return fmt.Errorf("unknown template kind '%s'", kind)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("template id is required")
	}
	if !isValidAutomationID(id) {
		return fmt.Errorf("template id '%s' contains invalid characters", id)
	}

	tpl := automationTemplate{
		Name:            strings.TrimSpace(view.Name),
		Description:     strings.TrimSpace(view.Description),
		Goal:            view.Goal,
		Title:           view.Title,
		Body:            view.Body,
		WorkerCount:     view.WorkerCount,
		TimeoutSeconds:  view.TimeoutSeconds,
		SeedFirstIntent: view.SeedFirstIntent,
		DedupBy:         strings.TrimSpace(view.DedupBy),
		CooldownSeconds: view.CooldownSeconds,
		SkipIfRunning:   view.SkipIfRunning,
		UpdatedAt:       time.Now().Unix(),
	}
	if tpl.Name == "" {
		tpl.Name = id
	}

	switch kind {
	case templateKindArtex:
		if strings.TrimSpace(tpl.Goal) == "" {
			return errors.New("artex template requires a goal")
		}
	case templateKindNotify:
		if strings.TrimSpace(tpl.Title) == "" && strings.TrimSpace(tpl.Body) == "" {
			return errors.New("notification template requires a title or a body")
		}
	}

	ts.automationMu.Lock()
	defer ts.automationMu.Unlock()

	next := cloneAutomationStore(ts.automationStore)
	if kind == templateKindArtex {
		next.Templates.Artex[id] = tpl
	} else {
		next.Templates.Notify[id] = tpl
	}

	if err := saveAutomationStore(next); err != nil {
		return err
	}
	ts.automationStore = next

	logs.Info("", "Automation: %s template '%s' saved", kind, id)
	return nil
}

// TsAutomationTemplateDelete removes a template, refusing when a rule still
// references it so a rule can never silently lose its configuration.
func (ts *Teamserver) TsAutomationTemplateDelete(kind, id string) error {
	if !validTemplateKind(kind) {
		return fmt.Errorf("unknown template kind '%s'", kind)
	}

	ts.automationMu.Lock()
	defer ts.automationMu.Unlock()

	source := ts.automationStore.Templates.Artex
	if kind == templateKindNotify {
		source = ts.automationStore.Templates.Notify
	}
	if _, exists := source[id]; !exists {
		return fmt.Errorf("unknown %s template '%s'", kind, id)
	}

	if referencedBy := ts.templateReferrersLocked(kind, id); len(referencedBy) > 0 {
		return fmt.Errorf("template '%s' is still used by rule(s): %s", id, strings.Join(referencedBy, ", "))
	}

	next := cloneAutomationStore(ts.automationStore)
	if kind == templateKindArtex {
		delete(next.Templates.Artex, id)
	} else {
		delete(next.Templates.Notify, id)
	}

	if err := saveAutomationStore(next); err != nil {
		return err
	}
	ts.automationStore = next

	logs.Info("", "Automation: %s template '%s' deleted", kind, id)
	return nil
}

// templateReferrersLocked lists the rule ids that reference a template. The
// caller must hold automationMu.
func (ts *Teamserver) templateReferrersLocked(kind, id string) []string {
	var referrers []string
	for _, rule := range ts.autoExecRules() {
		state := ts.automationStore.Rules[rule.ID]

		switch kind {
		case templateKindArtex:
			effective := state.ArtexTemplateID
			if effective == "" && rule.Artex != nil {
				effective = rule.Artex.TemplateID
			}
			if effective == id {
				referrers = append(referrers, rule.ID)
			}
		case templateKindNotify:
			effective := state.NotifyTemplateID
			if effective == "" && rule.Notify != nil {
				effective = rule.Notify.TemplateID
			}
			if effective == id {
				referrers = append(referrers, rule.ID)
			}
		}
	}
	return referrers
}

func automationTemplateView(kind, id string, tpl automationTemplate) profile.AutomationTemplateView {
	return profile.AutomationTemplateView{
		ID:              id,
		Kind:            kind,
		Name:            tpl.Name,
		Description:     tpl.Description,
		Goal:            tpl.Goal,
		Title:           tpl.Title,
		Body:            tpl.Body,
		WorkerCount:     tpl.WorkerCount,
		TimeoutSeconds:  tpl.TimeoutSeconds,
		SeedFirstIntent: tpl.SeedFirstIntent,
		DedupBy:         tpl.DedupBy,
		CooldownSeconds: tpl.CooldownSeconds,
		SkipIfRunning:   tpl.SkipIfRunning,
		UpdatedAt:       tpl.UpdatedAt,
	}
}

// automationArtexTemplate resolves an ARTEX template by id.
func (ts *Teamserver) automationArtexTemplate(id string) (automationTemplate, bool) {
	if id == "" {
		return automationTemplate{}, false
	}
	ts.automationMu.RLock()
	defer ts.automationMu.RUnlock()
	tpl, exists := ts.automationStore.Templates.Artex[id]
	return tpl, exists
}

// automationNotifyTemplate resolves a notification template by id.
func (ts *Teamserver) automationNotifyTemplate(id string) (automationTemplate, bool) {
	if id == "" {
		return automationTemplate{}, false
	}
	ts.automationMu.RLock()
	defer ts.automationMu.RUnlock()
	tpl, exists := ts.automationStore.Templates.Notify[id]
	return tpl, exists
}

// --- channels ----------------------------------------------------------------

// AutomationChannelSendKey returns the effective credential for a channel,
// falling back to the legacy profile block for ServerChan.
func (ts *Teamserver) AutomationChannelSendKey(id string) string {
	ts.automationMu.RLock()
	channel, exists := ts.automationStore.Channels[id]
	ts.automationMu.RUnlock()

	if exists && channel.SendKey != "" {
		return channel.SendKey
	}
	if id == NotifyChannelServerChan && ts.Profile != nil && ts.Profile.AutoExec != nil && ts.Profile.AutoExec.ServerChan != nil {
		return ts.Profile.AutoExec.ServerChan.SendKey
	}
	return ""
}

// AutomationChannelAPIBase returns the effective API base for a channel.
func (ts *Teamserver) AutomationChannelAPIBase(id string) string {
	ts.automationMu.RLock()
	channel, exists := ts.automationStore.Channels[id]
	ts.automationMu.RUnlock()

	if exists && channel.APIBase != "" {
		return channel.APIBase
	}
	if id == NotifyChannelServerChan && ts.Profile != nil && ts.Profile.AutoExec != nil && ts.Profile.AutoExec.ServerChan != nil {
		return ts.Profile.AutoExec.ServerChan.APIBase
	}
	return ""
}

// TsAutomationChannelSet updates a channel credential. An empty secret keeps
// the current value so the client can patch the API base alone.
func (ts *Teamserver) TsAutomationChannelSet(id, sendKey, apiBase string) error {
	if _, exists := ts.NotifyChannel(id); !exists {
		return fmt.Errorf("unknown notification channel '%s'", id)
	}
	sendKey = strings.TrimSpace(sendKey)
	apiBase = strings.TrimSpace(apiBase)

	ts.automationMu.Lock()
	defer ts.automationMu.Unlock()

	next := cloneAutomationStore(ts.automationStore)
	channel := next.Channels[id]
	if sendKey != "" {
		channel.SendKey = sendKey
	}
	if apiBase != "" {
		channel.APIBase = apiBase
	}
	next.Channels[id] = channel

	if err := saveAutomationStore(next); err != nil {
		return err
	}
	ts.automationStore = next

	logs.Info("", "Automation: channel '%s' configuration updated", id)
	return nil
}

// TsAutomationChannelsList returns the registered channels with their masked
// credentials. The plain secret never leaves the teamserver.
func (ts *Teamserver) TsAutomationChannelsList() []profile.AutomationChannelView {
	result := []profile.AutomationChannelView{}
	for _, channel := range ts.notifyChannels() {
		result = append(result, profile.AutomationChannelView{
			ID:          channel.ID(),
			Name:        channel.Name(),
			Configured:  channel.Configured(),
			Masked:      channel.Masked(),
			SecretLabel: channel.SecretLabel(),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// AutomationServerChanSendKey is kept for callers that predate the channel
// registry.
func (ts *Teamserver) AutomationServerChanSendKey() string {
	return ts.AutomationChannelSendKey(NotifyChannelServerChan)
}

// AutomationServerChanAPIBase is kept for callers that predate the channel
// registry.
func (ts *Teamserver) AutomationServerChanAPIBase() string {
	return ts.AutomationChannelAPIBase(NotifyChannelServerChan)
}

// AutomationServerChanConfigured reports whether a SendKey exists without
// ever exposing it.
func (ts *Teamserver) AutomationServerChanConfigured() bool {
	return ts.AutomationServerChanSendKey() != ""
}

// AutomationServerChanMask returns the masked SendKey for display in clients.
func (ts *Teamserver) AutomationServerChanMask() string {
	return maskSecret(ts.AutomationServerChanSendKey())
}

// TsAutomationServerChanSet is kept for callers that predate the channel
// registry.
func (ts *Teamserver) TsAutomationServerChanSet(sendKey, apiBase string) error {
	return ts.TsAutomationChannelSet(NotifyChannelServerChan, sendKey, apiBase)
}

// maskSecret renders a credential as a short mask such as "SCT4...fQR".
func maskSecret(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return key[:1] + "..." + key[len(key)-1:]
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// maskServerChanKey is kept for existing tests.
func maskServerChanKey(key string) string {
	return maskSecret(key)
}

// isValidAutomationID accepts template identifiers.
func isValidAutomationID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func toInt(value any) (int, error) {
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case float64:
		return int(typed), nil
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, err
		}
		return int(parsed), nil
	default:
		return 0, fmt.Errorf("expected a number, got %T", value)
	}
}

func toStringSlice(value any) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected a list of strings")
			}
			result = append(result, str)
		}
		return result, nil
	case string:
		// A multi-line editor submits one string; split it into commands.
		var result []string
		for _, line := range strings.Split(typed, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				result = append(result, line)
			}
		}
		return result, nil
	default:
		return nil, fmt.Errorf("expected a list of strings, got %T", value)
	}
}

func saveAutomationStore(store automationStore) error {
	if logs.RepoLogsInstance == nil {
		return errors.New("logs repository is not initialized")
	}

	if store.Version == 0 {
		store.Version = automationStoreVersion
	}
	if store.Rules == nil {
		store.Rules = make(map[string]automationRuleState)
	}
	if store.Templates.Artex == nil {
		store.Templates.Artex = make(map[string]automationTemplate)
	}
	if store.Templates.Notify == nil {
		store.Templates.Notify = make(map[string]automationTemplate)
	}
	if store.Channels == nil {
		store.Channels = make(map[string]automationChannelState)
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	dir := logs.RepoLogsInstance.DataPath
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".automation-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, automationStorePath())
}
