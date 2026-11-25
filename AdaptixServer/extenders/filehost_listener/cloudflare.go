package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	cfAPIBase       = "https://api.cloudflare.com/client/v4"
	defaultTunnelID = "21d02c65-e0c3-4b3b-8457-5ace81f88541"
)

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cfEnvelope struct {
	Success bool            `json:"success"`
	Errors  []cfError       `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type cfZone struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Account struct {
		ID string `json:"id"`
	} `json:"account"`
}

type cfDNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func cfToken(conf *TransportConfig) (string, error) {
	if token := strings.TrimSpace(conf.CFAPIToken); token != "" {
		return token, nil
	}
	path := strings.TrimSpace(conf.CFTokenFile)
	if path == "" {
		return "", errors.New("no Cloudflare token: set cf_token_file (recommended) or cf_api_token")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cloudflare: read token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("cloudflare: token file is empty")
	}
	return token, nil
}

// cfCall performs one API request and returns the raw result. Error messages
// never include the authorization token.
func cfCall(method, token, path string, body any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, cfAPIBase+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var envelope cfEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("cloudflare: %s %s: http %d invalid response", method, path, resp.StatusCode)
	}
	if !envelope.Success {
		message := "unknown error"
		if len(envelope.Errors) > 0 {
			message = envelope.Errors[0].Message
		}
		return nil, fmt.Errorf("cloudflare: %s %s: %s", method, path, message)
	}
	return envelope.Result, nil
}

func cfResolveZone(token, zoneName string) (zoneID, accountID string, err error) {
	result, err := cfCall("GET", token, "/zones?name="+url.QueryEscape(zoneName), nil)
	if err != nil {
		return "", "", err
	}
	var zones []cfZone
	if err := json.Unmarshal(result, &zones); err != nil {
		return "", "", fmt.Errorf("cloudflare: decode zones: %w", err)
	}
	if len(zones) == 0 {
		return "", "", fmt.Errorf("cloudflare: zone %q not found or token lacks access", zoneName)
	}
	return zones[0].ID, zones[0].Account.ID, nil
}

func cfFindDNS(token, zoneID, name string) ([]cfDNSRecord, error) {
	result, err := cfCall("GET", token, "/zones/"+zoneID+"/dns_records?name="+url.QueryEscape(name), nil)
	if err != nil {
		return nil, err
	}
	var records []cfDNSRecord
	if err := json.Unmarshal(result, &records); err != nil {
		return nil, fmt.Errorf("cloudflare: decode dns records: %w", err)
	}
	return records, nil
}

func cfCreateCNAME(token, zoneID, name, target string) (string, error) {
	body := map[string]any{
		"type":    "CNAME",
		"name":    name,
		"content": target,
		"proxied": true,
		"ttl":     1,
	}
	result, err := cfCall("POST", token, "/zones/"+zoneID+"/dns_records", body)
	if err != nil {
		return "", err
	}
	var record cfDNSRecord
	if err := json.Unmarshal(result, &record); err != nil {
		return "", fmt.Errorf("cloudflare: decode created record: %w", err)
	}
	return record.ID, nil
}

func cfDeleteDNS(token, zoneID, recordID string) error {
	_, err := cfCall("DELETE", token, "/zones/"+zoneID+"/dns_records/"+recordID, nil)
	return err
}

func cfGetTunnelConfig(token, accountID, tunnelID string) (map[string]any, int64, error) {
	result, err := cfCall("GET", token, "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/configurations", nil)
	if err != nil {
		return nil, 0, err
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, 0, fmt.Errorf("cloudflare: decode tunnel config: %w", err)
	}
	config, _ := payload["config"].(map[string]any)
	if config == nil {
		config = map[string]any{}
	}
	var version int64
	if v, ok := payload["version"].(float64); ok {
		version = int64(v)
	}
	return config, version, nil
}

func cfPutTunnelConfig(token, accountID, tunnelID string, config map[string]any, version int64) error {
	body := map[string]any{"config": config}
	if version > 0 {
		body["version"] = version
	}
	_, err := cfCall("PUT", token, "/accounts/"+accountID+"/cfd_tunnel/"+tunnelID+"/configurations", body)
	return err
}

// appendIngressRule returns a deep-copied config with the hostname rule added
// before the catch-all rule. The input config is never mutated.
func appendIngressRule(config map[string]any, hostname, service string) (map[string]any, bool, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, false, err
	}
	var next map[string]any
	if err := json.Unmarshal(raw, &next); err != nil {
		return nil, false, err
	}

	ingress := []any{}
	if existing, ok := next["ingress"]; ok && existing != nil {
		list, ok := existing.([]any)
		if !ok {
			return nil, false, errors.New("cloudflare: tunnel ingress is not a list")
		}
		ingress = list
	}

	insertAt := len(ingress)
	for i, item := range ingress {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if got, _ := rule["hostname"].(string); got == hostname {
			svc, _ := rule["service"].(string)
			if svc == service {
				return next, false, nil
			}
			rule["service"] = service
			return next, true, nil
		}
		if got, _ := rule["hostname"].(string); got == "" {
			insertAt = i
		}
	}

	rule := map[string]any{"hostname": hostname, "service": service}
	merged := make([]any, 0, len(ingress)+1)
	merged = append(merged, ingress[:insertAt]...)
	merged = append(merged, rule)
	merged = append(merged, ingress[insertAt:]...)
	next["ingress"] = merged
	return next, true, nil
}

func backupTunnelConfig(dir string, config map[string]any) {
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return
	}
	name := filepath.Join(dir, "cf_tunnel_backup_"+time.Now().Format("20060102-150405")+".json")
	_ = os.WriteFile(name, raw, 0600)
}

func provisionCloudflare(conf *TransportConfig, dataDir string) error {
	token, err := cfToken(conf)
	if err != nil {
		return err
	}

	domain := strings.TrimSpace(conf.ExternalDomain)
	if domain == "" {
		return nil
	}
	zoneName := strings.TrimSpace(conf.CFZone)
	if zoneName == "" {
		parts := strings.SplitN(domain, ".", 2)
		if len(parts) != 2 || parts[1] == "" {
			return errors.New("cloudflare: cannot derive cf_zone from external_domain; set cf_zone")
		}
		zoneName = parts[1]
	}
	tunnelID := strings.TrimSpace(conf.CFTunnelID)
	if tunnelID == "" {
		tunnelID = defaultTunnelID
	}

	zoneID, accountID, err := cfResolveZone(token, zoneName)
	if err != nil {
		return err
	}

	createdRecord := ""
	records, err := cfFindDNS(token, zoneID, domain)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		if !conf.AutoCreateDNS {
			return fmt.Errorf("cloudflare: DNS record for %s does not exist and auto_create_dns is disabled", domain)
		}
		createdRecord, err = cfCreateCNAME(token, zoneID, domain, tunnelID+".cfargotunnel.com")
		if err != nil {
			return err
		}
	}

	if !conf.UpdateTunnelIngress {
		return nil
	}

	config, version, err := cfGetTunnelConfig(token, accountID, tunnelID)
	if err != nil {
		if createdRecord != "" {
			_ = cfDeleteDNS(token, zoneID, createdRecord)
		}
		return err
	}

	service := fmt.Sprintf("http://127.0.0.1:%d", conf.PortBind)
	nextConfig, changed, err := appendIngressRule(config, domain, service)
	if err != nil {
		if createdRecord != "" {
			_ = cfDeleteDNS(token, zoneID, createdRecord)
		}
		return err
	}
	if !changed {
		return nil
	}

	backupTunnelConfig(dataDir, config)
	if err := cfPutTunnelConfig(token, accountID, tunnelID, nextConfig, version); err != nil {
		// Best-effort restore of the previous tunnel configuration.
		_ = cfPutTunnelConfig(token, accountID, tunnelID, config, version)
		if createdRecord != "" {
			_ = cfDeleteDNS(token, zoneID, createdRecord)
		}
		return err
	}
	return nil
}
