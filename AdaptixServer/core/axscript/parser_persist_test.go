package axscript

import (
	"strings"
	"testing"
)

func persistCommandDefs() *CommandDef {
	sshkey := CommandDef{
		Name: "sshkey",
		Args: []ArgumentDef{
			{Type: ArgTypeString, Name: "key", Required: true},
			{Type: ArgTypeString, Name: "user"},
		},
	}
	cron := CommandDef{
		Name: "cron",
		Args: []ArgumentDef{
			{Type: ArgTypeString, Name: "schedule", Required: true},
			{Type: ArgTypeString, Name: "payload", Required: true},
			{Type: ArgTypeString, Name: "user"},
			{Type: ArgTypeString, Name: "name"},
		},
	}
	systemd := CommandDef{
		Name: "systemd",
		Args: []ArgumentDef{
			{Type: ArgTypeString, Name: "payload", Required: true},
			{Type: ArgTypeString, Name: "user"},
			{Type: ArgTypeString, Name: "name"},
			{Type: ArgTypeInt, Name: "interval"},
		},
	}
	list := CommandDef{Name: "list"}
	remove := CommandDef{
		Name: "remove",
		Args: []ArgumentDef{
			{Type: ArgTypeString, Name: "method", Required: true},
			{Type: ArgTypeString, Name: "name", Required: true},
		},
	}

	return &CommandDef{
		Name:        "persist",
		Subcommands: []CommandDef{sshkey, cron, systemd, list, remove},
	}
}

func TestParsePersistSSHKey(t *testing.T) {
	cmdDef := persistCommandDefs()
	resolved := &ResolvedCommand{Command: cmdDef, Subcommand: &cmdDef.Subcommands[0]}

	parsed, err := ParseCommand(`persist sshkey "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample adaptix-persist" root`, resolved)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if parsed.SubcommandName != "sshkey" {
		t.Fatalf("subcommand = %q", parsed.SubcommandName)
	}
	if got := parsed.Args["key"]; got != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample adaptix-persist" {
		t.Fatalf("key = %q", got)
	}
	if got := parsed.Args["user"]; got != "root" {
		t.Fatalf("user = %q", got)
	}
}

func TestParsePersistCron(t *testing.T) {
	cmdDef := persistCommandDefs()
	resolved := &ResolvedCommand{Command: cmdDef, Subcommand: &cmdDef.Subcommands[1]}

	parsed, err := ParseCommand(`persist cron "*/5 * * * *" "/tmp/agent.bin --c2" root adaptix-cron`, resolved)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if parsed.SubcommandName != "cron" {
		t.Fatalf("subcommand = %q", parsed.SubcommandName)
	}
	if got := parsed.Args["schedule"]; got != "*/5 * * * *" {
		t.Fatalf("schedule = %q", got)
	}
	if got := parsed.Args["payload"]; got != "/tmp/agent.bin --c2" {
		t.Fatalf("payload = %q", got)
	}
	if got := parsed.Args["user"]; got != "root" {
		t.Fatalf("user = %q", got)
	}
	if got := parsed.Args["name"]; got != "adaptix-cron" {
		t.Fatalf("name = %q", got)
	}
}

func TestParsePersistSystemdWithInterval(t *testing.T) {
	cmdDef := persistCommandDefs()
	resolved := &ResolvedCommand{Command: cmdDef, Subcommand: &cmdDef.Subcommands[2]}

	parsed, err := ParseCommand(`persist systemd "/tmp/agent.bin" root adaptix-persist 600`, resolved)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := parsed.Args["payload"]; got != "/tmp/agent.bin" {
		t.Fatalf("payload = %q", got)
	}
	if got := parsed.Args["interval"]; got != 600 {
		t.Fatalf("interval = %v (%T)", got, got)
	}
}

func TestParsePersistList(t *testing.T) {
	cmdDef := persistCommandDefs()
	resolved := &ResolvedCommand{Command: cmdDef, Subcommand: &cmdDef.Subcommands[3]}

	parsed, err := ParseCommand(`persist list`, resolved)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if parsed.SubcommandName != "list" {
		t.Fatalf("subcommand = %q", parsed.SubcommandName)
	}
}

func TestParsePersistRemove(t *testing.T) {
	cmdDef := persistCommandDefs()
	resolved := &ResolvedCommand{Command: cmdDef, Subcommand: &cmdDef.Subcommands[4]}

	parsed, err := ParseCommand(`persist remove cron adaptix-cron`, resolved)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if parsed.SubcommandName != "remove" {
		t.Fatalf("subcommand = %q", parsed.SubcommandName)
	}
	if got := parsed.Args["method"]; got != "cron" {
		t.Fatalf("method = %q", got)
	}
	if got := parsed.Args["name"]; got != "adaptix-cron" {
		t.Fatalf("name = %q", got)
	}
}

func TestTokenizeKeepsQuotedArguments(t *testing.T) {
	tokens := Tokenize(`persist sshkey "ssh-ed25519 AAAAC3N key with spaces" root`)

	if len(tokens) != 4 {
		t.Fatalf("tokens = %v", tokens)
	}
	if tokens[2] != "ssh-ed25519 AAAAC3N key with spaces" {
		t.Fatalf("quoted token = %q", tokens[2])
	}
	if !strings.HasPrefix(tokens[0], "persist") {
		t.Fatalf("first token = %q", tokens[0])
	}
}
