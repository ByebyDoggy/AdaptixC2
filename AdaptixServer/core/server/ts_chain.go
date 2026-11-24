package server

import (
	"AdaptixServer/core/profile"
	"AdaptixServer/core/utils/logs"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/sha3"
)

const (
	chainConfigFileName = "chain.json"

	chainRPCTimeout     = 12 * time.Second
	chainMaxResponse    = 1 << 20
	chainMinPollSeconds = 15
	chainMaxPollSeconds = 86400
	chainDefaultPoll    = 60
)

func chainConfigPath() string {
	return filepath.Join(logs.RepoLogsInstance.DataPath, chainConfigFileName)
}

// loadChainConfig loads data/chain.json over the YAML defaults. It is called
// once during Teamserver.Start before any agent build can happen.
func (ts *Teamserver) loadChainConfig() {
	cfg := profile.TsChainConfig{PollSeconds: chainDefaultPoll}

	if ts.Profile != nil && ts.Profile.Server != nil && ts.Profile.Server.Chain != nil {
		cfg = *ts.Profile.Server.Chain
	}

	path := chainConfigPath()
	data, err := os.ReadFile(path)
	if err == nil {
		var stored profile.TsChainConfig
		if err := json.Unmarshal(data, &stored); err != nil {
			logs.Warn("", "Failed to parse %s: %s", path, err.Error())
		} else {
			cfg = stored
		}
	} else if !os.IsNotExist(err) {
		logs.Warn("", "Failed to read %s: %s", path, err.Error())
	}

	normalizeChainConfig(&cfg)

	ts.chainMu.Lock()
	ts.chainConfig = cfg
	ts.chainMu.Unlock()
}

func (ts *Teamserver) TsChainConfigGet() profile.TsChainConfig {
	ts.chainMu.RLock()
	defer ts.chainMu.RUnlock()
	return ts.chainConfig
}

func (ts *Teamserver) TsChainConfigSet(cfg profile.TsChainConfig) error {
	normalizeChainConfig(&cfg)
	if err := validateChainConfig(cfg); err != nil {
		return err
	}

	ts.chainMu.Lock()
	defer ts.chainMu.Unlock()

	if err := saveChainConfig(cfg); err != nil {
		return err
	}
	ts.chainConfig = cfg

	logs.Info("", "Chain config updated: enabled=%v domain=%s chain_id=%d poll=%ds",
		cfg.Enabled, cfg.Domain, cfg.ChainID, cfg.PollSeconds)
	return nil
}

// TsChainStatus reads domain/enabled/version/updatedAt from the configured
// contract. RPC failures are returned to the caller for display.
func (ts *Teamserver) TsChainStatus() (profile.TsChainStatus, error) {
	var status profile.TsChainStatus

	cfg := ts.TsChainConfigGet()
	if cfg.RPC == "" || cfg.Contract == "" {
		return status, errors.New("chain RPC or contract is not configured")
	}

	if cfg.ChainID > 0 {
		var raw string
		if err := chainRPC(cfg.RPC, "eth_chainId", []any{}, &raw); err != nil {
			return status, err
		}
		chainID, err := parseChainQuantity(raw)
		if err != nil {
			return status, err
		}
		if chainID != cfg.ChainID {
			return status, fmt.Errorf("chain id mismatch: rpc=%d config=%d", chainID, cfg.ChainID)
		}
	}

	domain, err := chainCallString(cfg.RPC, cfg.Contract, "domain()")
	if err != nil {
		return status, err
	}
	enabled, err := chainCallBool(cfg.RPC, cfg.Contract, "enabled()")
	if err != nil {
		return status, err
	}
	version, err := chainCallUint64(cfg.RPC, cfg.Contract, "version()")
	if err != nil {
		return status, err
	}
	updatedAt, err := chainCallUint64(cfg.RPC, cfg.Contract, "updatedAt()")
	if err != nil {
		return status, err
	}

	status.Domain = domain
	status.Enabled = enabled
	status.Version = version
	status.UpdatedAt = updatedAt

	return status, nil
}

// TsChainCalldata returns ABI-encoded setDomain(string) calldata for an
// external owner wallet. The teamserver never signs or broadcasts it.
func (ts *Teamserver) TsChainCalldata(domain string) (string, error) {
	domain = strings.TrimSpace(domain)
	if !validChainHost(domain) {
		return "", errors.New("invalid domain: expected host only (no scheme/path/port)")
	}

	return chainSelector("setDomain(string)") + hex.EncodeToString(encodeChainString(domain)), nil
}

func normalizeChainConfig(cfg *profile.TsChainConfig) {
	cfg.Domain = strings.TrimSpace(cfg.Domain)
	cfg.RPC = strings.TrimSpace(cfg.RPC)
	cfg.Contract = strings.TrimSpace(cfg.Contract)

	if cfg.PollSeconds == 0 {
		cfg.PollSeconds = chainDefaultPoll
	}
	if cfg.PollSeconds < chainMinPollSeconds {
		cfg.PollSeconds = chainMinPollSeconds
	}
	if cfg.PollSeconds > chainMaxPollSeconds {
		cfg.PollSeconds = chainMaxPollSeconds
	}
}

func validateChainConfig(cfg profile.TsChainConfig) error {
	if cfg.Enabled {
		if !validChainHost(cfg.Domain) {
			return errors.New("domain must be a host only (no scheme/path/port)")
		}
		if !validChainRPC(cfg.RPC) {
			return errors.New("rpc must be an http(s) URL")
		}
		if !validChainContract(cfg.Contract) {
			return errors.New("contract must be a 0x-prefixed 20-byte address")
		}
		if cfg.ChainID <= 0 {
			return errors.New("chain_id must be positive")
		}
	} else {
		if cfg.Domain != "" && !validChainHost(cfg.Domain) {
			return errors.New("domain must be a host only (no scheme/path/port)")
		}
		if cfg.RPC != "" && !validChainRPC(cfg.RPC) {
			return errors.New("rpc must be an http(s) URL")
		}
		if cfg.Contract != "" && !validChainContract(cfg.Contract) {
			return errors.New("contract must be a 0x-prefixed 20-byte address")
		}
	}

	if cfg.PollSeconds < chainMinPollSeconds || cfg.PollSeconds > chainMaxPollSeconds {
		return fmt.Errorf("poll_seconds must be between %d and %d", chainMinPollSeconds, chainMaxPollSeconds)
	}

	return nil
}

func saveChainConfig(cfg profile.TsChainConfig) error {
	if logs.RepoLogsInstance == nil {
		return errors.New("logs repository is not initialized")
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	dir := logs.RepoLogsInstance.DataPath
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".chain-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, chainConfigPath())
}

func chainCallString(rpcURL, contract, signature string) (string, error) {
	data, err := chainEthCall(rpcURL, contract, signature)
	if err != nil {
		return "", err
	}
	return decodeChainString(data)
}

func chainCallBool(rpcURL, contract, signature string) (bool, error) {
	data, err := chainEthCall(rpcURL, contract, signature)
	if err != nil {
		return false, err
	}
	return decodeChainBool(data)
}

func chainCallUint64(rpcURL, contract, signature string) (uint64, error) {
	data, err := chainEthCall(rpcURL, contract, signature)
	if err != nil {
		return 0, err
	}
	return decodeChainUint64(data)
}

func chainEthCall(rpcURL, contract, signature string) ([]byte, error) {
	params := []any{
		map[string]string{
			"to":   contract,
			"data": chainSelector(signature),
		},
		"latest",
	}

	var raw string
	if err := chainRPC(rpcURL, "eth_call", params, &raw); err != nil {
		return nil, err
	}

	raw = strings.TrimPrefix(raw, "0x")
	if raw == "" {
		return nil, errors.New("empty eth_call result")
	}

	return hex.DecodeString(raw)
}

type chainRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func chainRPC(rpcURL, method string, params any, result any) error {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, rpcURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: chainRPCTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("rpc http status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, chainMaxResponse))
	if err != nil {
		return err
	}

	var rpcResp chainRPCResponse
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return fmt.Errorf("rpc invalid json: %w", err)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	if len(rpcResp.Result) == 0 {
		return errors.New("rpc empty result")
	}

	return json.Unmarshal(rpcResp.Result, result)
}

func chainSelector(signature string) string {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write([]byte(signature))
	return "0x" + hex.EncodeToString(h.Sum(nil)[:4])
}

func parseChainQuantity(value string) (int64, error) {
	value = strings.TrimPrefix(value, "0x")
	if value == "" {
		return 0, errors.New("empty quantity")
	}

	n, ok := new(big.Int).SetString(value, 16)
	if !ok || !n.IsInt64() {
		return 0, errors.New("invalid quantity")
	}

	return n.Int64(), nil
}

func encodeChainString(value string) []byte {
	data := []byte(value)
	padding := (32 - len(data)%32) % 32

	out := make([]byte, 0, 64+len(data)+padding)
	out = append(out, chainWord(32)...)
	out = append(out, chainWord(uint64(len(data)))...)
	out = append(out, data...)
	out = append(out, make([]byte, padding)...)

	return out
}

func chainWord(n uint64) []byte {
	b := make([]byte, 32)
	binary.BigEndian.PutUint64(b[24:], n)
	return b
}

func decodeChainString(data []byte) (string, error) {
	if len(data) < 64 || len(data)%32 != 0 {
		return "", errors.New("abi: string data too short")
	}

	offset := new(big.Int).SetBytes(data[:32])
	if !offset.IsUint64() {
		return "", errors.New("abi: invalid string offset")
	}
	off := offset.Uint64()
	if off%32 != 0 || off > uint64(len(data)-32) {
		return "", errors.New("abi: string offset out of range")
	}

	length := new(big.Int).SetBytes(data[off : off+32])
	if !length.IsUint64() {
		return "", errors.New("abi: invalid string length")
	}
	size := length.Uint64()
	if size > uint64(len(data))-off-32 {
		return "", errors.New("abi: string length out of range")
	}

	return string(data[off+32 : off+32+size]), nil
}

func decodeChainBool(data []byte) (bool, error) {
	if len(data) < 32 {
		return false, errors.New("abi: bool data too short")
	}

	for _, b := range data[:31] {
		if b != 0 {
			return false, errors.New("abi: invalid bool")
		}
	}

	switch data[31] {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, errors.New("abi: invalid bool")
	}
}

func decodeChainUint64(data []byte) (uint64, error) {
	if len(data) < 32 {
		return 0, errors.New("abi: uint data too short")
	}

	value := new(big.Int).SetBytes(data[:32])
	if !value.IsUint64() {
		return 0, errors.New("abi: uint does not fit uint64")
	}

	return value.Uint64(), nil
}

func validChainHost(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	if host[0] == '.' || host[0] == '-' || host[len(host)-1] == '.' || host[len(host)-1] == '-' {
		return false
	}

	for i := 0; i < len(host); i++ {
		c := host[i]
		if c < 0x21 || c > 0x7e {
			return false
		}
		if c == '/' || c == ':' || c == '\\' {
			return false
		}
	}

	return true
}

func validChainRPC(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host != ""
}

func validChainContract(address string) bool {
	if len(address) != 42 {
		return false
	}
	if !strings.HasPrefix(address, "0x") && !strings.HasPrefix(address, "0X") {
		return false
	}
	_, err := hex.DecodeString(address[2:])
	return err == nil
}
