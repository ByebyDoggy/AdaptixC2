package profile

// ScriptArg describes one argument a script in the server-side script library
// accepts.
type ScriptArg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Default     string `json:"default"`
}

// ScriptManifest describes a script in the server-side script library. The
// library lives in the "scripts" directory of the teamserver and holds one
// "<id>.sh" body plus one "<id>.json" manifest per script.
type ScriptManifest struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	OS          []string    `json:"os"`
	Args        []ScriptArg `json:"args"`
	Destructive bool        `json:"destructive"`
	Version     string      `json:"version"`
}
