package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func abiWord(n uint64) []byte {
	b := make([]byte, 32)
	binary.BigEndian.PutUint64(b[24:], n)
	return b
}

func abiEncodeString(s string) string {
	data := []byte(s)
	padded := make([]byte, ((len(data)+31)/32)*32)
	copy(padded, data)

	out := append(abiWord(32), abiWord(uint64(len(data)))...)
	out = append(out, padded...)

	return "0x" + hex.EncodeToString(out)
}

func abiEncodeBool(v bool) string {
	b := make([]byte, 32)
	if v {
		b[31] = 1
	}
	return "0x" + hex.EncodeToString(b)
}

type mockChain struct {
	mu      sync.Mutex
	domain  string
	enabled bool
	fail    bool
	chainID string
}

func (m *mockChain) set(domain string, enabled bool) {
	m.mu.Lock()
	m.domain = domain
	m.enabled = enabled
	m.mu.Unlock()
}

func (m *mockChain) setFail(fail bool) {
	m.mu.Lock()
	m.fail = fail
	m.mu.Unlock()
}

func (m *mockChain) handler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	writeResult := func(result any) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  result,
		})
	}
	writeError := func(message string) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]any{
				"code":    -32000,
				"message": message,
			},
		})
	}

	switch req.Method {
	case "eth_chainId":
		m.mu.Lock()
		chainID := m.chainID
		m.mu.Unlock()
		writeResult(chainID)

	case "eth_call":
		m.mu.Lock()
		fail := m.fail
		domain := m.domain
		enabled := m.enabled
		m.mu.Unlock()

		if fail {
			writeError("mock rpc failure")
			return
		}
		if len(req.Params) == 0 {
			writeError("missing params")
			return
		}

		var call struct {
			To   string `json:"to"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal(req.Params[0], &call); err != nil {
			writeError("bad call params")
			return
		}

		switch call.Data {
		case selectorHex(chainSigDomain):
			writeResult(abiEncodeString(domain))
		case selectorHex(chainSigEnabled):
			writeResult(abiEncodeBool(enabled))
		default:
			writeError("unknown selector")
		}

	default:
		writeError("unknown method")
	}
}

func newTestResolver(t *testing.T, m *mockChain) *chainResolver {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(srv.Close)

	return newChainResolver(chainResolverConfig{
		RPC:          srv.URL,
		ChainID:      97,
		Contract:     "0x63cd2A5cB365170bB852f3A7864383800B62DAe5",
		PollInterval: time.Hour,
	})
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()

	data, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	return data
}

func TestSelectorHexKnownValues(t *testing.T) {
	if got := selectorHex(chainSigDomain); got != "0xc2fb26a6" {
		t.Fatalf("domain selector = %s, want 0xc2fb26a6", got)
	}
	if got := selectorHex(chainSigEnabled); got != "0x238dafe0" {
		t.Fatalf("enabled selector = %s, want 0x238dafe0", got)
	}
}

func TestChainResolverRewrite(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		enabled bool
		input   string
		want    string
	}{
		{
			name:    "host only",
			domain:  "new.example.com",
			enabled: true,
			input:   "wss://old.example.com/ws?k=abc",
			want:    "wss://new.example.com/ws?k=abc",
		},
		{
			name:    "port preserved",
			domain:  "new.example.com",
			enabled: true,
			input:   "wss://old.example.com:8443/ws?k=abc",
			want:    "wss://new.example.com:8443/ws?k=abc",
		},
		{
			name:    "ws scheme",
			domain:  "new.example.com",
			enabled: true,
			input:   "ws://old.example.com/ws",
			want:    "ws://new.example.com/ws",
		},
		{
			name:    "disabled uses embedded",
			domain:  "new.example.com",
			enabled: false,
			input:   "wss://old.example.com/ws?k=abc",
			want:    "wss://old.example.com/ws?k=abc",
		},
		{
			name:    "non websocket untouched",
			domain:  "new.example.com",
			enabled: true,
			input:   "tcp://old.example.com:9001",
			want:    "tcp://old.example.com:9001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockChain{domain: tt.domain, enabled: tt.enabled, chainID: "0x61"}
			r := newTestResolver(t, m)
			r.refresh()

			if got := r.Rewrite(tt.input); got != tt.want {
				t.Fatalf("Rewrite(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestChainResolverFallbackOnRPCError(t *testing.T) {
	m := &mockChain{domain: "new.example.com", enabled: true, chainID: "0x61", fail: true}
	r := newTestResolver(t, m)
	r.refresh()

	const embedded = "wss://old.example.com/ws?k=abc"
	if got := r.Rewrite(embedded); got != embedded {
		t.Fatalf("Rewrite during RPC failure = %q, want %q", got, embedded)
	}
}

func TestChainResolverFallbackOnInvalidHost(t *testing.T) {
	m := &mockChain{domain: "bad.example.com/path", enabled: true, chainID: "0x61"}
	r := newTestResolver(t, m)
	r.refresh()

	const embedded = "wss://old.example.com/ws"
	if got := r.Rewrite(embedded); got != embedded {
		t.Fatalf("Rewrite with invalid host = %q, want %q", got, embedded)
	}
}

func TestChainResolverFallbackOnChainIDMismatch(t *testing.T) {
	m := &mockChain{domain: "new.example.com", enabled: true, chainID: "0x1"}
	r := newTestResolver(t, m)
	r.refresh()

	const embedded = "wss://old.example.com/ws"
	if got := r.Rewrite(embedded); got != embedded {
		t.Fatalf("Rewrite with wrong chain = %q, want %q", got, embedded)
	}
}

func TestChainResolverClosesConnOnFirstResolution(t *testing.T) {
	m := &mockChain{domain: "new.example.com", enabled: true, chainID: "0x61"}
	r := newTestResolver(t, m)

	c1, c2 := net.Pipe()
	defer c2.Close()

	// Simulate the race where the agent connected to the embedded host before
	// the first RPC poll completed.
	if !r.SetActiveConn(c1, "wss://old.example.com/ws") {
		t.Fatal("expected pre-resolution connection to be registered")
	}
	r.refresh()

	_ = c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := c2.Read(buf); err == nil {
		t.Fatal("expected embedded connection to be closed after first resolution")
	}
}

func TestChainResolverRejectsConnToFallbackHost(t *testing.T) {
	m := &mockChain{domain: "new.example.com", enabled: true, chainID: "0x61"}
	r := newTestResolver(t, m)
	r.refresh()

	c1, c2 := net.Pipe()
	defer c2.Close()

	if r.SetActiveConn(c1, "wss://old.example.com/ws") {
		t.Fatal("expected connection to embedded host to be rejected")
	}

	_ = c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := c2.Read(buf); err == nil {
		t.Fatal("expected rejected connection to be closed")
	}
}

func TestChainResolverClosesConnOnDomainChange(t *testing.T) {
	m := &mockChain{domain: "old.example.com", enabled: true, chainID: "0x61"}
	r := newTestResolver(t, m)
	r.refresh()

	if got := r.Rewrite("wss://old.example.com/ws"); got != "wss://old.example.com/ws" {
		t.Fatalf("initial Rewrite = %q", got)
	}

	c1, c2 := net.Pipe()
	defer c2.Close()

	if !r.SetActiveConn(c1, "wss://old.example.com/ws") {
		t.Fatal("expected matching connection to be registered")
	}
	m.set("new.example.com", true)
	r.refresh()

	_ = c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := c2.Read(buf); err == nil {
		t.Fatal("expected active connection to be closed on domain change")
	}

	if got := r.Rewrite("wss://old.example.com/ws"); got != "wss://new.example.com/ws" {
		t.Fatalf("post-change Rewrite = %q", got)
	}
}

func TestDecodeABIString(t *testing.T) {
	valid := mustDecodeHex(t, abiEncodeString("tcp.sweeper.channel"))

	got, err := decodeABIString(valid)
	if err != nil {
		t.Fatalf("decode valid string: %v", err)
	}
	if got != "tcp.sweeper.channel" {
		t.Fatalf("decoded = %q", got)
	}

	bad := map[string][]byte{
		"short":           make([]byte, 32),
		"offset too far":  append(abiWord(64), make([]byte, 32)...),
		"unaligned":       append(abiWord(33), make([]byte, 32)...),
		"length too long": append(append(abiWord(32), abiWord(1000)...), make([]byte, 32)...),
	}

	for name, data := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeABIString(data); err == nil {
				t.Fatal("expected decode error")
			}
		})
	}
}

func TestDecodeABIBool(t *testing.T) {
	falseData := mustDecodeHex(t, abiEncodeBool(false))
	trueData := mustDecodeHex(t, abiEncodeBool(true))

	if v, err := decodeABIBool(falseData); err != nil || v {
		t.Fatalf("decode false = %v, %v", v, err)
	}
	if v, err := decodeABIBool(trueData); err != nil || !v {
		t.Fatalf("decode true = %v, %v", v, err)
	}

	invalid := mustDecodeHex(t, abiEncodeBool(false))
	invalid[31] = 2
	if _, err := decodeABIBool(invalid); err == nil {
		t.Fatal("expected invalid bool error")
	}

	highByte := mustDecodeHex(t, abiEncodeBool(false))
	highByte[0] = 1
	if _, err := decodeABIBool(highByte); err == nil {
		t.Fatal("expected high-byte bool error")
	}
}

func TestValidChainHost(t *testing.T) {
	valid := []string{
		"tcp.sweeper.channel",
		"c2.example.com",
		"localhost",
	}
	for _, host := range valid {
		if !validChainHost(host) {
			t.Fatalf("expected valid host %q", host)
		}
	}

	invalid := []string{
		"",
		"bad/path",
		"bad:port",
		"bad\\host",
		"a b",
		"-leading.example",
		"trailing.example-",
		".leading.example",
		"trailing.example.",
		strings.Repeat("a", 254),
	}
	for _, host := range invalid {
		if validChainHost(host) {
			t.Fatalf("expected invalid host %q", host)
		}
	}
}
