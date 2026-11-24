package server

import (
	"AdaptixServer/core/utils/logs"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"AdaptixServer/core/profile"

	"github.com/Adaptix-Framework/axc2"
	"golang.org/x/crypto/ssh"
)

const persistKeyID = "persist-ed25519"

// persistKeyDir is where the teamserver keeps keys it generates for
// persistence. The private key never leaves the server except through the
// explicit operator download endpoint.
func persistKeyDir() string {
	return filepath.Join(logs.RepoLogsInstance.DataPath, "keys")
}

func persistKeyPaths() (priv string, pub string) {
	priv = filepath.Join(persistKeyDir(), persistKeyID)
	return priv, priv + ".pub"
}

// ensurePersistKeypair returns the authorized_keys line for the persistence
// key, generating an ed25519 keypair on first use.
func (ts *Teamserver) ensurePersistKeypair() (string, error) {
	if logs.RepoLogsInstance == nil {
		return "", errors.New("logs repository is not initialized")
	}

	privPath, pubPath := persistKeyPaths()

	if data, err := os.ReadFile(pubPath); err == nil {
		if line := strings.TrimSpace(string(data)); line != "" {
			return line, nil
		}
	}

	dir := persistKeyDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", dir, err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}

	block, err := ssh.MarshalPrivateKey(priv, "adaptix-persist")
	if err != nil {
		return "", err
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", err
	}

	privPEM := pem.EncodeToMemory(block)
	pubLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " adaptix-persist"

	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", privPath, err)
	}
	if err := os.WriteFile(pubPath, []byte(pubLine+"\n"), 0644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", pubPath, err)
	}

	logs.Info("", "Generated persistence keypair at %s", privPath)
	return pubLine, nil
}

// TsPersistKeyInfo returns the key summary, generating the keypair if needed.
func (ts *Teamserver) TsPersistKeyInfo() (profile.PersistKeyView, error) {
	view := profile.PersistKeyView{ID: persistKeyID}

	pub, err := ts.ensurePersistKeypair()
	if err != nil {
		return view, err
	}
	view.PublicKey = pub

	privPath, _ := persistKeyPaths()
	if info, err := os.Stat(privPath); err == nil {
		view.Private = true
		view.CreatedAt = info.ModTime().Unix()
	}

	return view, nil
}

// TsPersistKeyPrivate returns the private key PEM for the operator to download.
func (ts *Teamserver) TsPersistKeyPrivate() (string, error) {
	if _, err := ts.ensurePersistKeypair(); err != nil {
		return "", err
	}
	privPath, _ := persistKeyPaths()
	data, err := os.ReadFile(privPath)
	if err != nil {
		return "", fmt.Errorf("persistence private key unavailable: %w", err)
	}
	logs.Warn("", "Persistence private key downloaded by an operator")
	return string(data), nil
}

// autofillScriptArgs fills the defaults an operator should never have to type.
// It is intentionally narrow: only scripts with a known one-click contract are
// touched, everything else passes through unchanged.
func (ts *Teamserver) autofillScriptArgs(agent adaptix.AgentData, scriptID string, args []string) []string {
	if scriptID != "persist_linux" {
		return args
	}

	has := func(flag string) bool {
		for _, arg := range args {
			if arg == flag {
				return true
			}
		}
		return false
	}
	value := func(flag string) string {
		for i := 0; i < len(args)-1; i++ {
			if args[i] == flag {
				return args[i+1]
			}
		}
		return ""
	}

	if !has("--method") {
		args = append(args, "--method", "all")
	}

	method := value("--method")
	if (method == "" || method == "all" || method == "sshkey") && !has("--key") && !has("--key-b64") {
		if pub, err := ts.ensurePersistKeypair(); err == nil {
			// The agent's "run" argument string loses shell quoting, so a key
			// line with spaces would be split into several tokens. Base64 has
			// no spaces and survives the round trip.
			args = append(args, "--key-b64", base64.StdEncoding.EncodeToString([]byte(pub)))
		} else {
			logs.Warn("", "Persistence key generation failed: %v", err)
		}
	}

	if !has("--agent-pid") && agent.Pid != "" {
		args = append(args, "--agent-pid", agent.Pid)
	}

	if !has("--name") {
		args = append(args, "--name", sanitizeArtifactName("adaptix-"+agent.Computer))
	}

	return args
}

// sanitizeArtifactName keeps a name safe for filenames and unit names.
func sanitizeArtifactName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		result = "adaptix-persist"
	}
	return result
}
