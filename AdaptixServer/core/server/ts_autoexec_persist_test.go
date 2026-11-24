package server

import (
	"strings"
	"testing"

	"AdaptixServer/core/axscript"
	"AdaptixServer/core/profile"
)

func TestBuildPersistCommandsSSHKey(t *testing.T) {
	ts := &Teamserver{}

	commands := ts.buildPersistCommands(profile.TsAutoExecPersist{
		SSHKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample key material adaptix-persist",
		User:   "root",
	})

	if len(commands) != 1 {
		t.Fatalf("commands = %v", commands)
	}
	if !strings.HasPrefix(commands[0], "persist sshkey ") {
		t.Fatalf("command = %q", commands[0])
	}

	tokens := axscript.Tokenize(commands[0])
	if len(tokens) != 4 {
		t.Fatalf("tokens = %v", tokens)
	}
	if tokens[0] != "persist" || tokens[1] != "sshkey" {
		t.Fatalf("tokens = %v", tokens)
	}
	if tokens[2] != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample key material adaptix-persist" {
		t.Fatalf("key token = %q", tokens[2])
	}
	if tokens[3] != "root" {
		t.Fatalf("user token = %q", tokens[3])
	}
}

func TestBuildPersistCommandsCron(t *testing.T) {
	ts := &Teamserver{}

	commands := ts.buildPersistCommands(profile.TsAutoExecPersist{
		Cron:     true,
		Schedule: "*/5 * * * *",
		Command:  "/tmp/agent.bin --c2",
		User:     "root",
		Name:     "adaptix-cron",
	})

	if len(commands) != 1 {
		t.Fatalf("commands = %v", commands)
	}

	tokens := axscript.Tokenize(commands[0])
	if len(tokens) != 6 {
		t.Fatalf("tokens = %v", tokens)
	}
	if tokens[0] != "persist" || tokens[1] != "cron" {
		t.Fatalf("tokens = %v", tokens)
	}
	if tokens[2] != "*/5 * * * *" {
		t.Fatalf("schedule token = %q", tokens[2])
	}
	if tokens[3] != "/tmp/agent.bin --c2" {
		t.Fatalf("command token = %q", tokens[3])
	}
	if tokens[4] != "root" || tokens[5] != "adaptix-cron" {
		t.Fatalf("tokens = %v", tokens)
	}
}

func TestBuildPersistCommandsSystemd(t *testing.T) {
	ts := &Teamserver{}

	commands := ts.buildPersistCommands(profile.TsAutoExecPersist{
		Systemd:  true,
		Command:  "/tmp/agent.bin --c2",
		User:     "root",
		Name:     "adaptix-persist",
		Interval: 600,
	})

	if len(commands) != 1 {
		t.Fatalf("commands = %v", commands)
	}

	tokens := axscript.Tokenize(commands[0])
	if len(tokens) != 6 {
		t.Fatalf("tokens = %v", tokens)
	}
	if tokens[0] != "persist" || tokens[1] != "systemd" {
		t.Fatalf("tokens = %v", tokens)
	}
	if tokens[2] != "/tmp/agent.bin --c2" {
		t.Fatalf("command token = %q", tokens[2])
	}
	if tokens[5] != "600" {
		t.Fatalf("interval token = %q", tokens[5])
	}
}

func TestBuildPersistCommandsCombinesMechanisms(t *testing.T) {
	ts := &Teamserver{}

	commands := ts.buildPersistCommands(profile.TsAutoExecPersist{
		SSHKey:   "ssh-ed25519 AAAAC3N test",
		Cron:     true,
		Schedule: "@reboot",
		Command:  "/tmp/agent.bin",
		Systemd:  true,
		Interval: 300,
	})

	if len(commands) != 3 {
		t.Fatalf("commands = %v", commands)
	}
	if !strings.HasPrefix(commands[0], "persist sshkey") {
		t.Fatalf("commands[0] = %q", commands[0])
	}
	if !strings.HasPrefix(commands[1], "persist cron") {
		t.Fatalf("commands[1] = %q", commands[1])
	}
	if !strings.HasPrefix(commands[2], "persist systemd") {
		t.Fatalf("commands[2] = %q", commands[2])
	}
}

func TestBuildPersistCommandsSkipsIncompletePolicies(t *testing.T) {
	ts := &Teamserver{}

	// Cron without a schedule or command must not produce a broken command.
	commands := ts.buildPersistCommands(profile.TsAutoExecPersist{
		Cron:    true,
		Command: "/tmp/agent.bin",
	})
	if len(commands) != 0 {
		t.Fatalf("commands = %v", commands)
	}

	// Systemd without a command must be skipped as well.
	commands = ts.buildPersistCommands(profile.TsAutoExecPersist{
		Systemd: true,
	})
	if len(commands) != 0 {
		t.Fatalf("commands = %v", commands)
	}

	// An empty policy produces nothing.
	commands = ts.buildPersistCommands(profile.TsAutoExecPersist{})
	if len(commands) != 0 {
		t.Fatalf("commands = %v", commands)
	}
}

func TestAxQuoteKeepsSpacesTogether(t *testing.T) {
	quoted := axQuote("value with spaces")

	tokens := axscript.Tokenize("cmd " + quoted)
	if len(tokens) != 2 {
		t.Fatalf("tokens = %v", tokens)
	}
	if tokens[1] != "value with spaces" {
		t.Fatalf("token = %q", tokens[1])
	}
}

func TestAxQuoteEscapesEmbeddedQuotes(t *testing.T) {
	quoted := axQuote(`say "hello"`)

	tokens := axscript.Tokenize("cmd " + quoted)
	if len(tokens) != 2 {
		t.Fatalf("tokens = %v", tokens)
	}
	if tokens[1] != `say "hello"` {
		t.Fatalf("token = %q", tokens[1])
	}
}
