package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"AdaptixServer/core/profile"
	"AdaptixServer/core/utils/logs"

	"github.com/Adaptix-Framework/axc2"
)

// scriptLibraryPath resolves the directory that holds the script library.
func (ts *Teamserver) scriptLibraryPath() string {
	dir := ""
	if ts.Profile != nil && ts.Profile.Server != nil {
		dir = strings.TrimSpace(ts.Profile.Server.ScriptsDir)
	}
	if dir == "" {
		dir = "scripts"
	}
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Join(logs.RepoLogsInstance.Path, dir)
}

// validateScriptID rejects anything that could escape the library directory.
func validateScriptID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("script id is required")
	}
	if strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return fmt.Errorf("invalid script id '%s'", id)
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return fmt.Errorf("invalid script id '%s'", id)
		}
	}
	return nil
}

// TsScriptsList returns the manifests of every script in the library.
func (ts *Teamserver) TsScriptsList() []profile.ScriptManifest {
	result := []profile.ScriptManifest{}

	dir := ts.scriptLibraryPath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		manifestPath := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			logs.Warn("", "Scripts: failed to read manifest %s: %s", manifestPath, err.Error())
			continue
		}

		var manifest profile.ScriptManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			logs.Warn("", "Scripts: invalid manifest %s: %s", manifestPath, err.Error())
			continue
		}
		if manifest.ID == "" {
			manifest.ID = strings.TrimSuffix(entry.Name(), ".json")
		}
		if manifest.Name == "" {
			manifest.Name = manifest.ID
		}
		if manifest.OS == nil {
			manifest.OS = []string{"linux"}
		}

		result = append(result, manifest)
	}

	return result
}

// scriptManifest loads one manifest by id.
func (ts *Teamserver) scriptManifest(scriptID string) (*profile.ScriptManifest, error) {
	if err := validateScriptID(scriptID); err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(ts.scriptLibraryPath(), scriptID+".json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("unknown script '%s'", scriptID)
	}

	var manifest profile.ScriptManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("invalid manifest for '%s': %w", scriptID, err)
	}
	if manifest.ID == "" {
		manifest.ID = scriptID
	}
	if manifest.Name == "" {
		manifest.Name = scriptID
	}
	if manifest.OS == nil {
		manifest.OS = []string{"linux"}
	}

	return &manifest, nil
}

// scriptContent reads the script body, refusing any path outside the library.
func (ts *Teamserver) scriptContent(scriptID string) ([]byte, error) {
	if err := validateScriptID(scriptID); err != nil {
		return nil, err
	}

	libDir := ts.scriptLibraryPath()
	scriptPath := filepath.Join(libDir, scriptID+".sh")

	real, err := filepath.EvalSymlinks(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("script '%s' not found", scriptID)
	}

	realLib, err := filepath.EvalSymlinks(libDir)
	if err != nil {
		realLib = libDir
	}

	rel, err := filepath.Rel(realLib, real)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, fmt.Errorf("script '%s' resolves outside the script library", scriptID)
	}

	return os.ReadFile(real)
}

// scriptSupportsOS reports whether a script may run on the given OS name.
func scriptSupportsOS(manifest *profile.ScriptManifest, osName string) bool {
	for _, candidate := range manifest.OS {
		if strings.EqualFold(candidate, osName) || candidate == "*" {
			return true
		}
	}
	return false
}

// scriptRemotePath is the deterministic landing path of an uploaded script.
func scriptRemotePath(scriptID string) string {
	return "/tmp/.adaptix-" + scriptID + ".sh"
}

// TsScriptRun pushes a script to the agent and executes it.
//
// The script body is uploaded with the agent's "upload" command and then
// started with "run". The agent's own shell is used, so the script must be
// compatible with the target OS.
func (ts *Teamserver) TsScriptRun(agentId, scriptID string, args []string) error {
	agent, err := ts.getAgent(agentId)
	if err != nil {
		return err
	}
	if !agent.Active {
		return fmt.Errorf("agent '%v' not active", agentId)
	}

	manifest, err := ts.scriptManifest(scriptID)
	if err != nil {
		return err
	}

	agentData := agent.GetData()
	osName := osToString(agentData.Os)
	if !scriptSupportsOS(manifest, osName) {
		return fmt.Errorf("script '%s' does not support %s (supported: %s)",
			scriptID, osName, strings.Join(manifest.OS, ", "))
	}

	content, err := ts.scriptContent(scriptID)
	if err != nil {
		return err
	}

	// Fill the defaults an operator should never have to type (persistence key,
	// agent pid, artifact name).
	args = ts.autofillScriptArgs(agentData, scriptID, args)

	if manifest.Destructive {
		logs.Warn("", "Scripts: running destructive script '%s' on agent %s", scriptID, agentId)
	}

	remotePath := scriptRemotePath(scriptID)

	// Upload the script body. The agent's "upload" command expects the file
	// content base64 encoded, so the body never has to be a server-side path.
	uploadArgs := map[string]any{
		"command":     "upload",
		"remote_path": remotePath,
		"local_file":  base64.StdEncoding.EncodeToString(content),
	}
	if err := ts.TsAgentCommand(agentData.Name, agentId, "scripts", "", "", "upload "+remotePath, false, uploadArgs); err != nil {
		return fmt.Errorf("failed to upload script '%s': %w", scriptID, err)
	}

	// Execute it. The uploaded file is 0644, so a direct fork/exec would fail
	// with EACCES; on Unix the script is handed to /bin/sh instead, which only
	// needs read permission.
	cmdline := buildRunCommand(agentData.Os, remotePath, args)

	if err := ts.TsAxScriptParseAndExecute(agentId, "scripts", cmdline); err != nil {
		return fmt.Errorf("failed to start script '%s': %w", scriptID, err)
	}

	logs.Info("", "Scripts: started '%s' on agent %s (%s)", scriptID, agentId, osName)
	return nil
}

// buildScriptCommands returns the command lines that upload and run a script.
// The upload carries the script body inline because the axscript file
// argument is restricted to the script sandbox.
func (ts *Teamserver) buildScriptCommands(scriptID string, args []string) ([]string, error) {
	if _, err := ts.scriptManifest(scriptID); err != nil {
		return nil, err
	}

	content, err := ts.scriptContent(scriptID)
	if err != nil {
		return nil, err
	}

	remotePath := scriptRemotePath(scriptID)

	uploadCmd := "upload " + axQuote(remotePath)
	runCmd := "run " + remotePath
	for _, arg := range args {
		runCmd += " " + axQuote(arg)
	}

	_ = content
	return []string{uploadCmd, runCmd}, nil
}

// scriptUploadArgs builds the agent arguments for the upload step. The
// "local_file" value carries the base64 encoded script body so the body never
// has to exist as a server-side path that the axscript sandbox would reject.
func scriptUploadArgs(scriptID string, content []byte) map[string]any {
	return map[string]any{
		"command":     "upload",
		"remote_path": scriptRemotePath(scriptID),
		"local_file":  base64.StdEncoding.EncodeToString(content),
	}
}

// buildRunCommand builds the agent "run" command line for an uploaded script.
// On Unix the interpreter is invoked explicitly because uploaded payloads land
// with mode 0644; Windows payloads are executed directly.
func buildRunCommand(agentOS int, remotePath string, args []string) string {
	cmd := "run "
	if agentOS == adaptix.OS_WINDOWS {
		cmd += axQuote(remotePath)
	} else {
		cmd += "/bin/sh " + axQuote(remotePath)
	}
	for _, arg := range args {
		cmd += " " + axQuote(arg)
	}
	return cmd
}

// scriptRunCommand builds the run command for a script.
func scriptRunCommand(scriptID string, args []string) string {
	cmd := "run " + scriptRemotePath(scriptID)
	for _, arg := range args {
		cmd += " " + axQuote(arg)
	}
	return cmd
}
