package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/sha3"
)

const (
	chainSigDomain  = "domain()"
	chainSigEnabled = "enabled()"

	chainDefaultPoll = 60 * time.Second
	chainMinPoll     = 15 * time.Second
	chainHTTPTimeout = 10 * time.Second
	chainMaxBody     = 1 << 20
)

type chainResolverConfig struct {
	RPC          string
	ChainID      int64
	Contract     string
	PollInterval time.Duration
}

// chainResolver polls an owner-controlled DomainRegistry contract and rewrites
// the host of wss:// / ws:// callback URLs. Embedded URLs remain the fallback.
type chainResolver struct {
	cfg    chainResolverConfig
	client *http.Client

	mu         sync.RWMutex
	enabled    bool
	domain     string
	lastErr    string
	lastGood   string
	lastUpdate time.Time
	chainOK    bool

	connMu sync.Mutex
	conn   net.Conn

	stop     chan struct{}
	stopOnce sync.Once
}

func newChainResolver(cfg chainResolverConfig) *chainResolver {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = chainDefaultPoll
	} else if cfg.PollInterval < chainMinPoll {
		cfg.PollInterval = chainMinPoll
	}

	return &chainResolver{
		cfg: cfg,
		client: &http.Client{
			Timeout: chainHTTPTimeout,
		},
		stop: make(chan struct{}),
	}
}

func (r *chainResolver) Start() {
	if r == nil {
		return
	}

	go func() {
		r.refresh()

		ticker := time.NewTicker(r.cfg.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				r.refresh()
			case <-r.stop:
				return
			}
		}
	}()
}

func (r *chainResolver) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		close(r.stop)
	})
}

// refresh reads domain()/enabled() from the contract. Any RPC, contract or
// validation failure disables the override so the embedded host is used.
func (r *chainResolver) refresh() {
	if err := r.verifyChain(); err != nil {
		r.setFailure(err)
		return
	}

	domain, err := r.callString(chainSigDomain)
	if err != nil {
		r.setFailure(err)
		return
	}

	enabled, err := r.callBool(chainSigEnabled)
	if err != nil {
		r.setFailure(err)
		return
	}

	if !validChainHost(domain) {
		r.setFailure(errors.New("contract returned invalid host"))
		return
	}

	r.apply(domain, enabled)
}

func (r *chainResolver) apply(domain string, enabled bool) {
	r.mu.Lock()
	oldEffective := r.effectiveLocked()
	r.domain = domain
	r.enabled = enabled
	r.lastGood = domain
	r.lastErr = ""
	r.lastUpdate = time.Now()
	newEffective := r.effectiveLocked()
	r.mu.Unlock()

	// A non-empty chain host that differs from the previous effective host
	// (including the first resolution after an embedded-host connection) must
	// force a reconnect; likewise a switch back to the embedded fallback.
	if oldEffective != newEffective {
		r.closeActiveConn()
	}
}

func (r *chainResolver) setFailure(err error) {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}

	r.mu.Lock()
	oldEffective := r.effectiveLocked()
	r.enabled = false
	r.lastErr = msg
	r.lastUpdate = time.Now()
	r.mu.Unlock()

	if oldEffective != "" {
		r.closeActiveConn()
	}
}

func (r *chainResolver) effectiveLocked() string {
	if r.enabled {
		return r.domain
	}
	return ""
}

// Rewrite replaces only the host of a ws:// or wss:// URL, preserving scheme,
// port, path and query. Non-WebSocket addresses and disabled/fallback states
// are returned unchanged.
func (r *chainResolver) Rewrite(addr string) string {
	if r == nil {
		return addr
	}

	r.mu.RLock()
	enabled, domain := r.enabled, r.domain
	r.mu.RUnlock()

	if !enabled || !validChainHost(domain) {
		return addr
	}

	u, err := url.Parse(addr)
	if err != nil {
		return addr
	}
	if u.Scheme != "wss" && u.Scheme != "ws" {
		return addr
	}
	if u.Hostname() == "" {
		return addr
	}

	if port := u.Port(); port != "" {
		u.Host = net.JoinHostPort(domain, port)
	} else {
		u.Host = domain
	}

	return u.String()
}

// SetActiveConn registers the live connection only if it was dialed to the
// currently effective host. This closes the race where the first RPC poll
// completes after the agent already connected to the embedded fallback host.
func (r *chainResolver) SetActiveConn(conn net.Conn, dialAddr string) bool {
	if r == nil {
		return true
	}

	r.mu.RLock()
	enabled, domain := r.enabled, r.domain
	r.mu.RUnlock()

	if enabled && validChainHost(domain) {
		if u, err := url.Parse(dialAddr); err == nil && (u.Scheme == "wss" || u.Scheme == "ws") {
			if !strings.EqualFold(u.Hostname(), domain) {
				_ = conn.Close()
				return false
			}
		}
	}

	r.connMu.Lock()
	r.conn = conn
	r.connMu.Unlock()
	return true
}

func (r *chainResolver) ClearActiveConn(conn net.Conn) {
	if r == nil {
		return
	}
	r.connMu.Lock()
	if r.conn == conn {
		r.conn = nil
	}
	r.connMu.Unlock()
}

func (r *chainResolver) closeActiveConn() {
	r.connMu.Lock()
	conn := r.conn
	r.conn = nil
	r.connMu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
}

func (r *chainResolver) verifyChain() error {
	if r.cfg.ChainID <= 0 {
		return nil
	}

	r.mu.RLock()
	ok := r.chainOK
	r.mu.RUnlock()
	if ok {
		return nil
	}

	var raw string
	if err := r.rpcCall("eth_chainId", []any{}, &raw); err != nil {
		return err
	}

	chainID, err := parseHexQuantity(raw)
	if err != nil {
		return err
	}
	if chainID != r.cfg.ChainID {
		return fmt.Errorf("unexpected chain id %d", chainID)
	}

	r.mu.Lock()
	r.chainOK = true
	r.mu.Unlock()
	return nil
}

func (r *chainResolver) callString(signature string) (string, error) {
	raw, err := r.ethCall(signature)
	if err != nil {
		return "", err
	}

	data, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
	if err != nil {
		return "", fmt.Errorf("abi: invalid hex: %w", err)
	}

	return decodeABIString(data)
}

func (r *chainResolver) callBool(signature string) (bool, error) {
	raw, err := r.ethCall(signature)
	if err != nil {
		return false, err
	}

	data, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
	if err != nil {
		return false, fmt.Errorf("abi: invalid hex: %w", err)
	}

	return decodeABIBool(data)
}

func (r *chainResolver) ethCall(signature string) (string, error) {
	params := []any{
		map[string]string{
			"to":   r.cfg.Contract,
			"data": selectorHex(signature),
		},
		"latest",
	}

	var result string
	if err := r.rpcCall("eth_call", params, &result); err != nil {
		return "", err
	}
	if result == "" || result == "0x" {
		return "", errors.New("eth_call returned empty result")
	}

	return result, nil
}

type chainRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type chainRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type chainRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *chainRPCError  `json:"error"`
}

func (r *chainResolver) rpcCall(method string, params any, result any) error {
	payload, err := json.Marshal(chainRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, r.cfg.RPC, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("rpc http status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, chainMaxBody))
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

func selectorHex(signature string) string {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write([]byte(signature))
	return "0x" + hex.EncodeToString(h.Sum(nil)[:4])
}

func parseHexQuantity(value string) (int64, error) {
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

// decodeABIString decodes a dynamically sized string return value:
// word(offset) | word(length) | padded bytes.
func decodeABIString(data []byte) (string, error) {
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

func decodeABIBool(data []byte) (bool, error) {
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

// validChainHost mirrors the contract-side validation: printable ASCII host
// only, 1..253 bytes, no scheme/path/port/backslash, no leading/trailing dot
// or hyphen.
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
