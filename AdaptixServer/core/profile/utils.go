package profile

type AdaptixProfile struct {
	Server     *TsProfile    `yaml:"Teamserver"`
	HttpServer *TsHttpServer `yaml:"HttpServer"`
	AutoExec   *TsAutoExec   `yaml:"auto_exec"`
}

type TsAutoExec struct {
	// OnAgentNew is the legacy rule list. Every entry is equivalent to a rule
	// in Rules with the trigger "agent.online".
	OnAgentNew []TsAutoExecRule `yaml:"on_agent_new"`

	// Rules holds automation rules that may fire on any trigger timing.
	Rules []TsAutoExecRule `yaml:"rules"`

	// ServerChan is the legacy location of the default push credential. It is
	// migrated into the runtime channel store on load.
	ServerChan *TsServerChanConfig `yaml:"serverchan"`
}

// TsServerChanConfig holds the ServerChan push credential. The teamserver is
// the only holder of SendKey; it is never returned to clients.
type TsServerChanConfig struct {
	SendKey string `yaml:"send_key"`
	APIBase string `yaml:"api_base"`
}

type TsAutoExecRule struct {
	ID          string             `yaml:"id"`
	Name        string             `yaml:"name"`
	Description string             `yaml:"description"`
	Order       int                `yaml:"order"`
	Trigger     string             `yaml:"trigger"`
	Agent       string             `yaml:"agent"`
	Listener    string             `yaml:"listener"`
	OS          string             `yaml:"os"`
	Tag         string             `yaml:"tag"`
	Commands    []string           `yaml:"commands"`
	DelaySec    int                `yaml:"delay_seconds"`
	Artex       *TsAutoExecArtex   `yaml:"artex"`
	Persist     *TsAutoExecPersist `yaml:"persist"`
	Notify      *TsAutoExecNotify  `yaml:"notify"`
	Script      string             `yaml:"script"`
	ScriptArgs  []string           `yaml:"script_args"`
}

// TsAutoExecNotify turns a rule into a push notification. A rule that only
// carries notify has no execution side effects and can therefore be toggled
// independently from execution rules.
type TsAutoExecNotify struct {
	// Title is the legacy inline title prefix.
	Title string `yaml:"title"`

	// ChannelID selects the notification channel implementation.
	ChannelID string `yaml:"channel_id"`

	// TemplateID selects a global notification template.
	TemplateID string `yaml:"template_id"`
}

// TsAutoExecPersist describes the persistence mechanisms installed on a newly
// checked-in agent. The SSH public key is supplied by the operator; the
// teamserver never stores a private key.
type TsAutoExecPersist struct {
	SSHKey   string `yaml:"ssh_key"`
	User     string `yaml:"user"`
	Cron     bool   `yaml:"cron"`
	Schedule string `yaml:"schedule"`
	Command  string `yaml:"command"`
	Systemd  bool   `yaml:"systemd"`
	Interval int    `yaml:"interval"`
	Name     string `yaml:"name"`
}

type TsAutoExecArtex struct {
	URL             string `yaml:"url"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	Name            string `yaml:"name"`
	WorkerCount     int    `yaml:"worker_count"`
	SeedFirstIntent bool   `yaml:"seed_first_intent"`
	TimeoutSeconds  int    `yaml:"timeout_seconds"`
	Template        string `yaml:"template"`
	DedupBy         string `yaml:"dedup_by"`
	CooldownSeconds int    `yaml:"cooldown_seconds"`
	SkipIfRunning   bool   `yaml:"skip_if_running"`

	// TemplateID selects a global ARTEX template. It takes precedence over
	// the inline Template above.
	TemplateID string `yaml:"template_id"`
}

type TsProfile struct {
	Interface      string            `yaml:"interface"`
	Port           int               `yaml:"port"`
	Endpoint       string            `yaml:"endpoint"`
	Password       string            `yaml:"password"`
	ManagePassword string            `yaml:"manage_password"`
	OnlyPassword   bool              `yaml:"only_password"`
	Operators      map[string]string `yaml:"operators"`
	Cert           string            `yaml:"cert"`
	Key            string            `yaml:"key"`
	Extenders      []string          `yaml:"extenders"`
	AxScripts      []string          `yaml:"axscripts"`
	ScriptsDir     string            `yaml:"scripts_dir"`
	ATokenLive     int               `yaml:"access_token_live_hours"`
	RTokenLive     int               `yaml:"refresh_token_live_hours"`
	Chain          *TsChainConfig    `yaml:"chain"`
}

// TsChainConfig controls the optional on-chain C2 domain resolver. The
// teamserver stores this config but never holds an owner key or signs
// transactions.
type TsChainConfig struct {
	Enabled     bool   `yaml:"enabled" json:"enabled"`
	Domain      string `yaml:"domain" json:"domain"`
	RPC         string `yaml:"rpc" json:"rpc"`
	ChainID     int64  `yaml:"chain_id" json:"chain_id"`
	Contract    string `yaml:"contract" json:"contract"`
	PollSeconds int    `yaml:"poll_seconds" json:"poll_seconds"`
}

// TsChainStatus is the on-chain state read back through eth_call.
type TsChainStatus struct {
	Domain    string `json:"domain"`
	Enabled   bool   `json:"enabled"`
	Version   uint64 `json:"version"`
	UpdatedAt uint64 `json:"updated_at"`
}

type TsHttpServer struct {
	Error *TsHttpError  `yaml:"error"`
	HTTP  *TsHTTPConfig `yaml:"http"`
	TLS   *TsTLSConfig  `yaml:"tls"`
}

type TsHttpError struct {
	Status      int               `yaml:"status"`
	Headers     map[string]string `yaml:"headers"`
	PagePath    string            `yaml:"page"`
	PageContent string
}

type TsHTTPConfig struct {
	MaxHeaderBytes        int    `yaml:"max_header_bytes"`
	ReadHeaderTimeoutSec  int    `yaml:"read_header_timeout_sec"`
	ReadTimeoutSec        int    `yaml:"read_timeout_sec"`
	WriteTimeoutSec       int    `yaml:"write_timeout_sec"`
	IdleTimeoutSec        int    `yaml:"idle_timeout_sec"`
	RequestTimeoutSec     int    `yaml:"request_timeout_sec"`
	RequestTimeoutMessage string `yaml:"request_timeout_message"`
	DisableKeepAlives     bool   `yaml:"disable_keep_alives"`
	EnableHTTP2           *bool  `yaml:"enable_http2"`
}

type TsTLSConfig struct {
	MinVersion               string   `yaml:"min_version"`
	MaxVersion               string   `yaml:"max_version"`
	PreferServerCipherSuites *bool    `yaml:"prefer_server_cipher_suites"`
	CipherSuites             []string `yaml:"cipher_suites"`
}
