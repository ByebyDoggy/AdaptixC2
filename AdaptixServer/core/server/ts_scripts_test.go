package server

import (
	"AdaptixServer/core/profile"
	"AdaptixServer/core/utils/logs"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/Adaptix-Framework/axc2"
)

// newScriptTestTeamserver builds a Teamserver whose script library lives in a
// temporary directory.
func newScriptTestTeamserver(t *testing.T) (*Teamserver, string) {
	t.Helper()

	dir := t.TempDir()
	oldRepo := logs.RepoLogsInstance
	logs.RepoLogsInstance = &logs.RepoLogs{Path: dir, DataPath: filepath.Join(dir, "data")}
	t.Cleanup(func() { logs.RepoLogsInstance = oldRepo })

	ts := &Teamserver{
		Profile: &profile.AdaptixProfile{
			Server: &profile.TsProfile{ScriptsDir: "scripts"},
		},
	}
	return ts, filepath.Join(dir, "scripts")
}

func writeScript(t *testing.T, dir, id, body, manifest string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".sh"), []byte(body), 0755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(manifest), 0644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
	}
}

func TestScriptsListReadsManifests(t *testing.T) {
	ts, dir := newScriptTestTeamserver(t)

	writeScript(t, dir, "persist_linux", "#!/bin/sh\necho hi\n",
		`{"id":"persist_linux","name":"Linux persist","os":["linux"],"destructive":false}`)
	writeScript(t, dir, "cleanup_traces", "#!/bin/sh\necho bye\n",
		`{"id":"cleanup_traces","name":"Clean traces","os":["linux"],"destructive":true}`)

	scripts := ts.TsScriptsList()
	if len(scripts) != 2 {
		t.Fatalf("scripts = %d", len(scripts))
	}

	byID := map[string]profile.ScriptManifest{}
	for _, s := range scripts {
		byID[s.ID] = s
	}

	persist, ok := byID["persist_linux"]
	if !ok {
		t.Fatal("persist_linux missing")
	}
	if persist.Name != "Linux persist" {
		t.Fatalf("name = %q", persist.Name)
	}
	if len(persist.OS) != 1 || persist.OS[0] != "linux" {
		t.Fatalf("os = %v", persist.OS)
	}
	if persist.Destructive {
		t.Fatal("persist_linux must not be destructive")
	}

	traces, ok := byID["cleanup_traces"]
	if !ok {
		t.Fatal("cleanup_traces missing")
	}
	if !traces.Destructive {
		t.Fatal("cleanup_traces must be destructive")
	}
}

func TestScriptsListEmptyWhenDirectoryMissing(t *testing.T) {
	ts, _ := newScriptTestTeamserver(t)

	if scripts := ts.TsScriptsList(); len(scripts) != 0 {
		t.Fatalf("scripts = %v", scripts)
	}
}

func TestValidateScriptIDRejectsTraversal(t *testing.T) {
	invalid := []string{"", "..", "../etc/passwd", "a/b", "a\\b", "has space", "semi;colon"}
	for _, id := range invalid {
		if err := validateScriptID(id); err == nil {
			t.Fatalf("expected rejection for %q", id)
		}
	}

	valid := []string{"persist_linux", "cleanup-traces", "a.b_c", "x1"}
	for _, id := range valid {
		if err := validateScriptID(id); err != nil {
			t.Fatalf("expected acceptance for %q: %v", id, err)
		}
	}
}

func TestScriptContentRejectsSymlinkEscape(t *testing.T) {
	ts, dir := newScriptTestTeamserver(t)

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	outside := filepath.Join(filepath.Dir(dir), "outside.sh")
	if err := os.WriteFile(outside, []byte("echo bad\n"), 0755); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	link := filepath.Join(dir, "escape.sh")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := ts.scriptContent("escape"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestScriptSupportsOS(t *testing.T) {
	manifest := &profile.ScriptManifest{OS: []string{"linux", "macos"}}

	if !scriptSupportsOS(manifest, "linux") {
		t.Fatal("linux should be supported")
	}
	if !scriptSupportsOS(manifest, "MACOS") {
		t.Fatal("matching should be case insensitive")
	}
	if scriptSupportsOS(manifest, "windows") {
		t.Fatal("windows should not be supported")
	}

	all := &profile.ScriptManifest{OS: []string{"*"}}
	if !scriptSupportsOS(all, "windows") {
		t.Fatal("wildcard should match")
	}
}

func TestScriptRunRejectsUnknownScript(t *testing.T) {
	ts, _ := newScriptTestTeamserver(t)

	if err := ts.TsScriptRun("agent1", "does_not_exist", nil); err == nil {
		t.Fatal("expected unknown script error")
	}
}

func TestScriptRunRejectsUnknownAgent(t *testing.T) {
	ts, dir := newScriptTestTeamserver(t)
	writeScript(t, dir, "demo", "#!/bin/sh\necho hi\n", `{"id":"demo","os":["linux"]}`)

	if err := ts.TsScriptRun("no-such-agent", "demo", nil); err == nil {
		t.Fatal("expected unknown agent error")
	}
}

func TestBuildScriptCommands(t *testing.T) {
	ts, dir := newScriptTestTeamserver(t)
	writeScript(t, dir, "demo", "#!/bin/sh\necho hi\n", `{"id":"demo","os":["linux"]}`)

	commands, err := ts.buildScriptCommands("demo", []string{"--method", "list"})
	if err != nil {
		t.Fatalf("buildScriptCommands: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %v", commands)
	}
	if commands[0] != `upload "/tmp/.adaptix-demo.sh"` {
		t.Fatalf("upload command = %q", commands[0])
	}
	if commands[1] != `run /tmp/.adaptix-demo.sh "--method" "list"` {
		t.Fatalf("run command = %q", commands[1])
	}
}

func TestScriptUploadArgsCarryBase64Body(t *testing.T) {
	args := scriptUploadArgs("demo", []byte("#!/bin/sh\necho hi\n"))

	if args["command"] != "upload" {
		t.Fatalf("command = %v", args["command"])
	}
	if args["remote_path"] != "/tmp/.adaptix-demo.sh" {
		t.Fatalf("remote_path = %v", args["remote_path"])
	}

	encoded, ok := args["local_file"].(string)
	if !ok || encoded == "" {
		t.Fatalf("local_file = %v", args["local_file"])
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(decoded) != "#!/bin/sh\necho hi\n" {
		t.Fatalf("decoded = %q", string(decoded))
	}
}

func TestBuildRunCommandUsesShellOnUnix(t *testing.T) {
	cmd := buildRunCommand(adaptix.OS_LINUX, "/tmp/.adaptix-demo.sh", []string{"--method", "all"})
	want := `run /bin/sh "/tmp/.adaptix-demo.sh" "--method" "all"`
	if cmd != want {
		t.Fatalf("buildRunCommand = %q, want %q", cmd, want)
	}
}

func TestBuildRunCommandExecutesWindowsDirectly(t *testing.T) {
	cmd := buildRunCommand(adaptix.OS_WINDOWS, "C:\\Temp\\demo.ps1", nil)
	want := `run "C:\Temp\demo.ps1"`
	if cmd != want {
		t.Fatalf("buildRunCommand = %q, want %q", cmd, want)
	}
}
