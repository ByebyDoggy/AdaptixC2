package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type TransportConfig struct {
	HostBind string `json:"host_bind"`
	PortBind int    `json:"port_bind"`
	RootDir  string `json:"root_dir"`

	PathToken      string `json:"path_token"`
	ExternalDomain string `json:"external_domain"`

	CFZone      string `json:"cf_zone"`
	CFTunnelID  string `json:"cf_tunnel_id"`
	CFTokenFile string `json:"cf_token_file"`
	CFAPIToken  string `json:"cf_api_token"`

	AutoCreateDNS       bool `json:"auto_create_dns"`
	UpdateTunnelIngress bool `json:"update_tunnel_ingress"`

	MaxFileMB         int      `json:"max_file_mb"`
	LinkExpiryMinutes int      `json:"link_expiry_minutes"`
	IPAllowlist       []string `json:"ip_allowlist"`
	PageError         string   `json:"page_error"`
	CreatedAt         int64    `json:"created_at"`
}

var hostnameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`)

const defaultErrorPage = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>404 - Page Not Found</title></head>
<body style="background:#121212;color:#fff;text-align:center;font-family:Arial,sans-serif">
<h1>AdaptixC2 404</h1><p>You need to enter the correct connection details.</p>
</body></html>`

func validConfig(config string) error {
	var conf TransportConfig
	if err := json.Unmarshal([]byte(config), &conf); err != nil {
		return err
	}
	if strings.TrimSpace(conf.HostBind) == "" {
		return errors.New("host_bind is required")
	}
	if conf.PortBind < 1 || conf.PortBind > 65535 {
		return errors.New("port_bind must be in the range 1-65535")
	}
	if strings.TrimSpace(conf.RootDir) == "" {
		return errors.New("root_dir is required")
	}
	if conf.PathToken != "" && (len(conf.PathToken) < 8 || len(conf.PathToken) > 128) {
		return errors.New("path_token must be 8-128 characters")
	}
	if conf.MaxFileMB < 0 {
		return errors.New("max_file_mb must be >= 0")
	}
	if conf.LinkExpiryMinutes < 0 {
		return errors.New("link_expiry_minutes must be >= 0")
	}
	if conf.ExternalDomain != "" && !hostnameRe.MatchString(conf.ExternalDomain) {
		return errors.New("external_domain must be a bare hostname")
	}
	return nil
}

func (c TransportConfig) PublicURL() string {
	if c.ExternalDomain != "" {
		return "https://" + c.ExternalDomain + "/" + c.PathToken + "/"
	}
	return "http://" + net.JoinHostPort(c.HostBind, strconv.Itoa(c.PortBind)) + "/" + c.PathToken + "/"
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

type TransportFileHost struct {
	Name   string
	Config TransportConfig
	Active bool

	mu     sync.RWMutex
	server *http.Server
}

func (t *TransportFileHost) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Active {
		return nil
	}
	if err := os.MkdirAll(t.Config.RootDir, 0700); err != nil {
		return fmt.Errorf("filehost: create root_dir: %w", err)
	}
	addr := net.JoinHostPort(t.Config.HostBind, strconv.Itoa(t.Config.PortBind))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("filehost: listen %s: %w", addr, err)
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           http.HandlerFunc(t.handle),
		ReadHeaderTimeout: 10 * time.Second,
	}
	t.server = server
	t.Active = true
	go func() {
		_ = server.Serve(listener)
	}()
	return nil
}

func (t *TransportFileHost) Stop() error {
	t.mu.Lock()
	server := t.server
	t.server = nil
	t.Active = false
	t.mu.Unlock()
	if server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

func (t *TransportFileHost) handle(w http.ResponseWriter, r *http.Request) {
	t.mu.RLock()
	conf := t.Config
	t.mu.RUnlock()

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		t.pageError(w, conf)
		return
	}
	if !ipAllowed(clientIP(r), conf.IPAllowlist) {
		t.pageError(w, conf)
		return
	}
	if conf.LinkExpiryMinutes > 0 && conf.CreatedAt > 0 {
		expiry := conf.CreatedAt + int64(conf.LinkExpiryMinutes)*60
		if time.Now().Unix() > expiry {
			t.pageError(w, conf)
			return
		}
	}

	prefix := "/" + conf.PathToken + "/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		t.pageError(w, conf)
		return
	}

	full, err := resolveFile(conf.RootDir, strings.TrimPrefix(r.URL.Path, prefix))
	if err != nil {
		t.pageError(w, conf)
		return
	}
	info, err := os.Stat(full)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		t.pageError(w, conf)
		return
	}
	if conf.MaxFileMB > 0 && info.Size() > int64(conf.MaxFileMB)*1024*1024 {
		t.pageError(w, conf)
		return
	}

	// Payloads must never be served from an edge cache: a stale binary is
	// worse than a slow one. Cloudflare honours these headers on proxied
	// hostnames.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("CDN-Cache-Control", "no-store")
	w.Header().Set("Cloudflare-CDN-Cache-Control", "no-store")

	http.ServeFile(w, r, full)
}

func (t *TransportFileHost) pageError(w http.ResponseWriter, conf TransportConfig) {
	body := conf.PageError
	if strings.TrimSpace(body) == "" {
		body = defaultErrorPage
	}
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Server", "AdaptixC2")
	w.Header().Set("Adaptix-Version", "v1.2")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, body)
}

// resolveFile maps a URL file name to a real path inside root and rejects
// traversal, nested paths, hidden files and symlink escapes.
func resolveFile(root, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "\x00") {
		return "", errors.New("invalid file name")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return "", errors.New("nested paths are not served")
	}
	if strings.HasPrefix(name, ".") {
		return "", errors.New("hidden files are not served")
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		rootReal = rootAbs
	}
	full := filepath.Join(rootReal, name)
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootReal, real)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes root_dir")
	}
	return real, nil
}

func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func ipAllowed(ip string, allow []string) bool {
	if len(allow) == 0 {
		return true
	}
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	for _, entry := range allow {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, cidr, err := net.ParseCIDR(entry); err == nil && cidr.Contains(parsed) {
				return true
			}
			continue
		}
		if other := net.ParseIP(entry); other != nil && other.Equal(parsed) {
			return true
		}
	}
	return false
}
