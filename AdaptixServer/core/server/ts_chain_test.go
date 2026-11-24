package server

import (
	"AdaptixServer/core/profile"
	"AdaptixServer/core/utils/logs"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const testChainContract = "0x63cd2A5cB365170bB852f3A7864383800B62DAe5"

func TestChainSelectorsKnownValues(t *testing.T) {
	tests := map[string]string{
		"domain()":          "0xc2fb26a6",
		"enabled()":         "0x238dafe0",
		"version()":         "0x54fd4d50",
		"updatedAt()":       "0x7519ab50",
		"setDomain(string)": "0xe5eab096",
	}

	for signature, want := range tests {
		if got := chainSelector(signature); got != want {
			t.Fatalf("selector(%q) = %s, want %s", signature, got, want)
		}
	}
}

func TestTsChainCalldata(t *testing.T) {
	ts := &Teamserver{}

	got, err := ts.TsChainCalldata("tcp.sweeper.channel")
	if err != nil {
		t.Fatalf("calldata: %v", err)
	}

	const want = "0xe5eab096" +
		"0000000000000000000000000000000000000000000000000000000000000020" +
		"0000000000000000000000000000000000000000000000000000000000000013" +
		"7463702e737765657065722e6368616e6e656c00000000000000000000000000"

	if got != want {
		t.Fatalf("calldata mismatch\n got: %s\nwant: %s", got, want)
	}

	invalid := []string{"", "https://example.com", "a:b", "bad/path"}
	for _, domain := range invalid {
		if _, err := ts.TsChainCalldata(domain); err == nil {
			t.Fatalf("expected error for domain %q", domain)
		}
	}
}

func TestNormalizeChainConfig(t *testing.T) {
	cfg := profile.TsChainConfig{
		Domain:      "  tcp.sweeper.channel  ",
		RPC:         " https://rpc.example.com ",
		Contract:    " " + testChainContract + " ",
		PollSeconds: 0,
	}
	normalizeChainConfig(&cfg)

	if cfg.Domain != "tcp.sweeper.channel" || cfg.RPC != "https://rpc.example.com" || cfg.Contract != testChainContract {
		t.Fatalf("trim failed: %+v", cfg)
	}
	if cfg.PollSeconds != chainDefaultPoll {
		t.Fatalf("poll = %d, want %d", cfg.PollSeconds, chainDefaultPoll)
	}

	cfg.PollSeconds = 1
	normalizeChainConfig(&cfg)
	if cfg.PollSeconds != chainMinPollSeconds {
		t.Fatalf("min poll = %d", cfg.PollSeconds)
	}

	cfg.PollSeconds = chainMaxPollSeconds + 1
	normalizeChainConfig(&cfg)
	if cfg.PollSeconds != chainMaxPollSeconds {
		t.Fatalf("max poll = %d", cfg.PollSeconds)
	}
}

func TestValidateChainConfig(t *testing.T) {
	valid := profile.TsChainConfig{
		Enabled:     true,
		Domain:      "tcp.sweeper.channel",
		RPC:         "https://data-seed-prebsc-1-s1.bnbchain.org:8545",
		ChainID:     97,
		Contract:    testChainContract,
		PollSeconds: 60,
	}
	if err := validateChainConfig(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	disabledEmpty := profile.TsChainConfig{Enabled: false, PollSeconds: 60}
	if err := validateChainConfig(disabledEmpty); err != nil {
		t.Fatalf("disabled empty config rejected: %v", err)
	}

	tests := map[string]func(*profile.TsChainConfig){
		"missing domain":   func(c *profile.TsChainConfig) { c.Domain = "" },
		"scheme in domain": func(c *profile.TsChainConfig) { c.Domain = "https://x.example" },
		"bad rpc":          func(c *profile.TsChainConfig) { c.RPC = "ftp://x" },
		"bad contract":     func(c *profile.TsChainConfig) { c.Contract = "0x1234" },
		"zero chain id":    func(c *profile.TsChainConfig) { c.ChainID = 0 },
		"low poll":         func(c *profile.TsChainConfig) { c.PollSeconds = 1 },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := valid
			mutate(&cfg)
			if err := validateChainConfig(cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestChainConfigSaveUses0600(t *testing.T) {
	dir := t.TempDir()
	oldRepo := logs.RepoLogsInstance
	logs.RepoLogsInstance = &logs.RepoLogs{DataPath: dir}
	t.Cleanup(func() { logs.RepoLogsInstance = oldRepo })

	cfg := profile.TsChainConfig{
		Enabled:     true,
		Domain:      "tcp.sweeper.channel",
		RPC:         "https://rpc.example.com",
		ChainID:     97,
		Contract:    testChainContract,
		PollSeconds: 60,
	}

	if err := saveChainConfig(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	path := filepath.Join(dir, chainConfigFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var stored profile.TsChainConfig
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stored != cfg {
		t.Fatalf("stored = %+v, want %+v", stored, cfg)
	}
}

func TestLoadChainConfigPrefersFile(t *testing.T) {
	dir := t.TempDir()
	oldRepo := logs.RepoLogsInstance
	logs.RepoLogsInstance = &logs.RepoLogs{DataPath: dir}
	t.Cleanup(func() { logs.RepoLogsInstance = oldRepo })

	fileCfg := profile.TsChainConfig{
		Enabled:     true,
		Domain:      "file.example.com",
		RPC:         "https://rpc.example.com",
		ChainID:     97,
		Contract:    testChainContract,
		PollSeconds: 30,
	}
	data, _ := json.Marshal(fileCfg)
	if err := os.WriteFile(filepath.Join(dir, chainConfigFileName), data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	ts := &Teamserver{
		Profile: &profile.AdaptixProfile{
			Server: &profile.TsProfile{
				Chain: &profile.TsChainConfig{
					Enabled:     true,
					Domain:      "yaml.example.com",
					RPC:         "https://yaml.example.com",
					ChainID:     97,
					Contract:    testChainContract,
					PollSeconds: 60,
				},
			},
		},
	}
	ts.loadChainConfig()

	if got := ts.TsChainConfigGet(); got.Domain != "file.example.com" {
		t.Fatalf("loaded domain = %q, want file.example.com", got.Domain)
	}
}

func TestDecodeChainValues(t *testing.T) {
	encoded := encodeChainString("tcp.sweeper.channel")
	got, err := decodeChainString(encoded)
	if err != nil {
		t.Fatalf("decode string: %v", err)
	}
	if got != "tcp.sweeper.channel" {
		t.Fatalf("decoded = %q", got)
	}

	word := make([]byte, 32)
	word[31] = 1
	if v, err := decodeChainBool(word); err != nil || !v {
		t.Fatalf("decode bool = %v, %v", v, err)
	}
	if v, err := decodeChainUint64(word); err != nil || v != 1 {
		t.Fatalf("decode uint = %v, %v", v, err)
	}

	overflow := make([]byte, 32)
	overflow[0] = 1
	if _, err := decodeChainUint64(overflow); err == nil {
		t.Fatal("expected uint overflow error")
	}

	badBool := make([]byte, 32)
	badBool[31] = 2
	if _, err := decodeChainBool(badBool); err == nil {
		t.Fatal("expected invalid bool error")
	}
}

func TestChainInputValidation(t *testing.T) {
	if !validChainContract(testChainContract) {
		t.Fatal("deployed contract rejected")
	}
	for _, addr := range []string{"", "0x1234", testChainContract + "00", "zz" + testChainContract[2:]} {
		if validChainContract(addr) {
			t.Fatalf("invalid contract accepted: %q", addr)
		}
	}

	for _, rpc := range []string{"https://rpc.example.com", "http://127.0.0.1:8545"} {
		if !validChainRPC(rpc) {
			t.Fatalf("valid rpc rejected: %q", rpc)
		}
	}
	for _, rpc := range []string{"", "ftp://x", "https://", "rpc.example.com"} {
		if validChainRPC(rpc) {
			t.Fatalf("invalid rpc accepted: %q", rpc)
		}
	}
}

func TestEncodeChainStringPadding(t *testing.T) {
	encoded := encodeChainString("abc")
	if len(encoded) != 96 {
		t.Fatalf("len = %d, want 96", len(encoded))
	}
	if hex.EncodeToString(encoded[:32]) != "0000000000000000000000000000000000000000000000000000000000000020" {
		t.Fatal("offset word incorrect")
	}
}
