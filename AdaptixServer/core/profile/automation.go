package profile

// AutomationActions summarises what a rule does, so the client can show the
// blast radius next to the toggle without exposing rule internals.
type AutomationActions struct {
	Commands      int    `json:"commands"`
	Persist       bool   `json:"persist"`
	Artex         bool   `json:"artex"`
	ArtexTemplate string `json:"artex_template"`
	Notify        bool   `json:"notify"`
	NotifyChannel string `json:"notify_channel"`
	Script        string `json:"script"`
}

// AutomationRuleView is the client-facing representation of one auto_exec rule.
// It carries the effective configuration (profile value overridden by the
// runtime store) so the client can render a full rule editor without guessing.
// It lives in the profile package so both the teamserver and the HTTP connector
// can reference it without an import cycle.
type AutomationRuleView struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Order            int               `json:"order"`
	Trigger          string            `json:"trigger"`
	TriggerName      string            `json:"trigger_name"`
	Agent            string            `json:"agent"`
	Listener         string            `json:"listener"`
	OS               string            `json:"os"`
	Tag              string            `json:"tag"`
	DelaySeconds     int               `json:"delay_seconds"`
	Enabled          bool              `json:"enabled"`
	Commands         []string          `json:"commands"`
	ArtexTemplateID  string            `json:"artex_template_id"`
	NotifyChannelID  string            `json:"notify_channel_id"`
	NotifyTemplateID string            `json:"notify_template_id"`
	Script           string            `json:"script"`
	ScriptArgs       []string          `json:"script_args"`
	Actions          AutomationActions `json:"actions"`
	LastRun          int64             `json:"last_run"`
	LastStatus       string            `json:"last_status"`
	LastMessage      string            `json:"last_message"`
}

// AutomationTemplateView is a global named template. ARTEX templates use Goal
// plus the task parameters; notification templates use Title and Body.
type AutomationTemplateView struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Goal            string `json:"goal"`
	Title           string `json:"title"`
	Body            string `json:"body"`
	WorkerCount     int    `json:"worker_count"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	SeedFirstIntent bool   `json:"seed_first_intent"`
	DedupBy         string `json:"dedup_by"`
	CooldownSeconds int    `json:"cooldown_seconds"`
	SkipIfRunning   bool   `json:"skip_if_running"`
	UpdatedAt       int64  `json:"updated_at"`
}

// AutomationChannelView is a notification channel as seen by clients. The
// credential itself is never included, only whether it exists and its mask.
type AutomationChannelView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Configured  bool   `json:"configured"`
	Masked      string `json:"masked"`
	SecretLabel string `json:"secret_label"`
}

// AutomationTriggerView describes one selectable trigger timing.
type AutomationTriggerView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PersistKeyView summarises the teamserver-generated persistence key. The
// private key is never part of this view.
type PersistKeyView struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
	Private   bool   `json:"private_available"`
	CreatedAt int64  `json:"created_at"`
}
