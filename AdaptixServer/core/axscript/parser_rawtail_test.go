package axscript

import "testing"

// The trailing free-form argument of `run` must keep its quoting: the agent
// re-splits it with shlex, and a value with spaces (an SSH public key, a path
// with spaces) only survives when the quotes are intact.
func TestParseCommandWideArgsKeepRawQuoting(t *testing.T) {
	resolved := &ResolvedCommand{
		Command: &CommandDef{
			Name: "run",
			Args: []ArgumentDef{
				{Name: "program", Type: ArgTypeString, Required: true},
				{Name: "args", Type: ArgTypeString},
			},
		},
	}

	cmdline := `run /bin/sh "/tmp/x.sh" "--key" "ssh-ed25519 AAAAC3 example" "--agent-pid" "42"`
	parsed, err := ParseCommand(cmdline, resolved)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}

	if got := parsed.Args["program"]; got != "/bin/sh" {
		t.Fatalf("program = %v, want /bin/sh", got)
	}
	want := `"/tmp/x.sh" "--key" "ssh-ed25519 AAAAC3 example" "--agent-pid" "42"`
	if got := parsed.Args["args"]; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

// Unquoted tails must stay byte-identical to the old join behaviour.
func TestParseCommandWideArgsPlainTail(t *testing.T) {
	resolved := &ResolvedCommand{
		Command: &CommandDef{
			Name: "run",
			Args: []ArgumentDef{
				{Name: "program", Type: ArgTypeString, Required: true},
				{Name: "args", Type: ArgTypeString},
			},
		},
	}

	parsed, err := ParseCommand("run /bin/echo hello world", resolved)
	if err != nil {
		t.Fatalf("ParseCommand: %v", err)
	}
	if got := parsed.Args["args"]; got != "hello world" {
		t.Fatalf("args = %q, want %q", got, "hello world")
	}
}
