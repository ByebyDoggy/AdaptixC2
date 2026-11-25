package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendIngressRule(t *testing.T) {
	config := map[string]any{
		"ingress": []any{
			map[string]any{"hostname": "ac2.sweeper.channel", "service": "https://127.0.0.1:4321"},
			map[string]any{"service": "http_status:404"},
		},
	}

	next, changed, err := appendIngressRule(config, "files.sweeper.channel", "http://127.0.0.1:18080")
	if err != nil || !changed {
		t.Fatalf("append failed: changed=%v err=%v", changed, err)
	}
	ingress := next["ingress"].([]any)
	if len(ingress) != 3 {
		t.Fatalf("want 3 rules, got %d", len(ingress))
	}
	last := ingress[2].(map[string]any)
	if last["service"] != "http_status:404" {
		t.Fatalf("catch-all must stay last, got %v", last)
	}
	if got := ingress[1].(map[string]any)["hostname"]; got != "files.sweeper.channel" {
		t.Fatalf("rule inserted at wrong position: %v", got)
	}
	if len(config["ingress"].([]any)) != 2 {
		t.Fatal("original config was mutated")
	}

	if _, changed, _ := appendIngressRule(next, "files.sweeper.channel", "http://127.0.0.1:18080"); changed {
		t.Fatal("identical rule must be idempotent")
	}
	_, changed, _ = appendIngressRule(next, "files.sweeper.channel", "http://127.0.0.1:18081")
	if !changed {
		t.Fatal("existing hostname with a new service must update")
	}
}

func TestResolveFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.bin"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveFile(dir, "agent.bin")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(resolved) != "agent.bin" {
		t.Fatalf("unexpected path %s", resolved)
	}

	for _, bad := range []string{"", "../etc/passwd", "..", ".hidden", "a/b", "a\\b"} {
		if _, err := resolveFile(dir, bad); err == nil {
			t.Fatalf("expected rejection for %q", bad)
		}
	}

	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("s"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveFile(dir, "link"); err == nil {
		t.Fatal("symlink escape must be rejected")
	}
}

func TestIPAllowed(t *testing.T) {
	if !ipAllowed("1.2.3.4", nil) {
		t.Fatal("empty allowlist must allow")
	}
	if !ipAllowed("1.2.3.4", []string{"1.2.3.0/24"}) {
		t.Fatal("CIDR match failed")
	}
	if ipAllowed("5.6.7.8", []string{"1.2.3.0/24"}) {
		t.Fatal("out-of-range IP allowed")
	}
	if !ipAllowed("::1", []string{"::1"}) {
		t.Fatal("IPv6 exact match failed")
	}
	if ipAllowed("not-an-ip", []string{"1.2.3.0/24"}) {
		t.Fatal("garbage IP must not match")
	}
}

func TestValidConfig(t *testing.T) {
	bad, _ := json.Marshal(map[string]any{"host_bind": "", "port_bind": 18080, "root_dir": "/tmp"})
	if validConfig(string(bad)) == nil {
		t.Fatal("expected host_bind error")
	}
	badPort, _ := json.Marshal(map[string]any{"host_bind": "127.0.0.1", "port_bind": 0, "root_dir": "/tmp"})
	if validConfig(string(badPort)) == nil {
		t.Fatal("expected port error")
	}
	good, _ := json.Marshal(map[string]any{
		"host_bind": "127.0.0.1", "port_bind": 18080, "root_dir": "/tmp",
		"external_domain": "files.sweeper.channel",
	})
	if err := validConfig(string(good)); err != nil {
		t.Fatalf("expected valid config: %v", err)
	}
}

func TestPublicURL(t *testing.T) {
	conf := TransportConfig{HostBind: "127.0.0.1", PortBind: 18080, PathToken: "abc123", ExternalDomain: "files.sweeper.channel"}
	if got := conf.PublicURL(); got != "https://files.sweeper.channel/abc123/" {
		t.Fatalf("unexpected public url %s", got)
	}
	conf.ExternalDomain = ""
	if got := conf.PublicURL(); got != "http://127.0.0.1:18080/abc123/" {
		t.Fatalf("unexpected local url %s", got)
	}
}
