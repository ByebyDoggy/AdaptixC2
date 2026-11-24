package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Adaptix-Framework/axc2"
)

func TestServerChanUID(t *testing.T) {
	tests := map[string]string{
		"sctp123tABC": "123",
		"sctp9tZZZ":   "9",
		"SCTP42tQ":    "42",
		"sctpNoUid":   "",
		"sct123tABC":  "",
	}
	for key, want := range tests {
		if got := serverChanUID(key); got != want {
			t.Fatalf("serverChanUID(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestServerChanEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		cfg     serverChanConfig
		want    string
		wantErr bool
	}{
		{
			name: "turbo key",
			cfg:  serverChanConfig{SendKey: "SCT123abc"},
			want: "https://sctapi.ftqq.com/SCT123abc.send",
		},
		{
			name: "sctp key",
			cfg:  serverChanConfig{SendKey: "sctp123tABCDEF"},
			want: "https://123.push.ft07.com/send/sctp123tABCDEF.send",
		},
		{
			name: "api base override",
			cfg:  serverChanConfig{SendKey: "SCT123abc", APIBase: "http://127.0.0.1:18080/"},
			want: "http://127.0.0.1:18080/SCT123abc.send",
		},
		{
			name:    "empty key",
			cfg:     serverChanConfig{},
			wantErr: true,
		},
		{
			name:    "sctp without uid",
			cfg:     serverChanConfig{SendKey: "sctpNoUid"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := serverChanEndpoint(&tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("endpoint: %v", err)
			}
			if got != tt.want {
				t.Fatalf("endpoint = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSendServerChanSuccess(t *testing.T) {
	var gotTitle, gotDesp string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotTitle = r.FormValue("title")
		gotDesp = r.FormValue("desp")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": ""})
	}))
	defer srv.Close()

	cfg := &serverChanConfig{SendKey: "SCTtest", APIBase: srv.URL}
	if err := sendServerChan(cfg, "hello", "**body**"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotTitle != "hello" {
		t.Fatalf("title = %q", gotTitle)
	}
	if gotDesp != "**body**" {
		t.Fatalf("desp = %q", gotDesp)
	}
}

func TestSendServerChanTruncatesTitle(t *testing.T) {
	var gotTitle string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotTitle = r.FormValue("title")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
	}))
	defer srv.Close()

	cfg := &serverChanConfig{SendKey: "SCTtest", APIBase: srv.URL}
	longTitle := strings.Repeat("中", 50)
	if err := sendServerChan(cfg, longTitle, "body"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if utf8.RuneCountInString(gotTitle) != serverChanTitleMax {
		t.Fatalf("title rune count = %d, want %d", utf8.RuneCountInString(gotTitle), serverChanTitleMax)
	}
}

func TestSendServerChanNonZeroCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 40001, "message": "bad sendkey"})
	}))
	defer srv.Close()

	cfg := &serverChanConfig{SendKey: "SCTtest", APIBase: srv.URL}
	err := sendServerChan(cfg, "title", "body")
	if err == nil {
		t.Fatal("expected error for non-zero code")
	}
	if !strings.Contains(err.Error(), "40001") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestSendServerChanHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := &serverChanConfig{SendKey: "SCTtest", APIBase: srv.URL}
	err := sendServerChan(cfg, "title", "body")
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildNotifyPayload(t *testing.T) {
	agent := adaptix.AgentData{
		Id:         "a1b2c3d4",
		Name:       "gopher",
		Listener:   "cfgopher",
		Computer:   "target-host",
		Username:   "root",
		Elevated:   true,
		InternalIP: "10.0.0.5",
		ExternalIP: "203.0.113.9",
		OsDesc:     "Ubuntu 22.04",
		Process:    "agent.bin",
		Pid:        "4242",
		CreateTime: 1795271372,
	}

	title, desp := buildNotifyPayload(agent, "ALERT")

	if title != "ALERT target-host" {
		t.Fatalf("title = %q", title)
	}

	required := []string{
		"target-host", "gopher (a1b2c3d4)", "cfgopher",
		"root", "是", "10.0.0.5", "203.0.113.9",
		"Ubuntu 22.04", "agent.bin", "PID 4242",
	}
	for _, value := range required {
		if !strings.Contains(desp, value) {
			t.Fatalf("desp missing %q:\n%s", value, desp)
		}
	}
}

func TestBuildNotifyPayloadWithoutPrefix(t *testing.T) {
	agent := adaptix.AgentData{Id: "id1", Computer: "host1"}
	title, _ := buildNotifyPayload(agent, "")
	if title != "AdaptixC2 上线: host1" {
		t.Fatalf("title = %q", title)
	}
}

func TestBuildNotifyPayloadEscapesTablePipes(t *testing.T) {
	agent := adaptix.AgentData{Id: "id1", Computer: "host|with|pipes"}
	_, desp := buildNotifyPayload(agent, "")
	if !strings.Contains(desp, `host\|with\|pipes`) {
		t.Fatalf("pipes not escaped:\n%s", desp)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("abc", 5); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := truncateRunes("abcdef", 3); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := truncateRunes("中文测试", 2); got != "中文" {
		t.Fatalf("got %q", got)
	}
}
