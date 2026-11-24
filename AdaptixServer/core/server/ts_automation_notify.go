package server

import (
	"AdaptixServer/core/profile"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Adaptix-Framework/axc2"
)

const (
	serverChanTimeout  = 10 * time.Second
	serverChanMaxBody  = 1 << 20
	serverChanTitleMax = 32
)

// serverChanConfig is the credential shape the ServerChan transport needs.
type serverChanConfig struct {
	SendKey string
	APIBase string
}

// serverChanEndpoint derives the push URL from the SendKey prefix.
//
// Server酱Turbo keys ("SCT...") use https://sctapi.ftqq.com/<key>.send.
// Server酱³ keys ("sctp<uid>t...") use https://<uid>.push.ft07.com/send/<key>.send.
// APIBase overrides the host and is intended for tests.
func serverChanEndpoint(cfg *serverChanConfig) (string, error) {
	if cfg == nil {
		return "", errors.New("serverchan is not configured")
	}

	key := strings.TrimSpace(cfg.SendKey)
	if key == "" {
		return "", errors.New("serverchan send_key is empty")
	}

	escaped := url.PathEscape(key)

	if base := strings.TrimRight(strings.TrimSpace(cfg.APIBase), "/"); base != "" {
		return base + "/" + escaped + ".send", nil
	}

	if strings.HasPrefix(strings.ToLower(key), "sctp") {
		uid := serverChanUID(key)
		if uid == "" {
			return "", errors.New("cannot derive uid from sctp send_key")
		}
		return fmt.Sprintf("https://%s.push.ft07.com/send/%s.send", uid, escaped), nil
	}

	return "https://sctapi.ftqq.com/" + escaped + ".send", nil
}

// serverChanUID extracts the numeric uid embedded in an sctp-prefixed key.
func serverChanUID(key string) string {
	rest := key
	if len(rest) >= 4 && strings.EqualFold(rest[:4], "sctp") {
		rest = rest[4:]
	}

	var b strings.Builder
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sendServerChan posts one notification. Failures are returned so the caller
// can record them; they must never abort sibling actions.
func sendServerChan(cfg *serverChanConfig, title, desp string) error {
	endpoint, err := serverChanEndpoint(cfg)
	if err != nil {
		return err
	}

	form := url.Values{}
	form.Set("title", truncateRunes(title, serverChanTitleMax))
	form.Set("desp", desp)

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: serverChanTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("serverchan request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, serverChanMaxBody))
	if err != nil {
		return fmt.Errorf("serverchan read: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("serverchan http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("serverchan decode: %w (body=%s)", err, strings.TrimSpace(string(body)))
	}
	if result.Code != 0 {
		return fmt.Errorf("serverchan code %d: %s", result.Code, result.Message)
	}

	return nil
}

// agentPlaceholders returns the substitution map shared by every template kind.
func agentPlaceholders(agent adaptix.AgentData) map[string]string {
	return map[string]string{
		"AGENT_ID":          agent.Id,
		"AGENT_TYPE":        agent.Name,
		"AGENT_HOST":        agent.Computer,
		"AGENT_USER":        agent.Username,
		"AGENT_OS":          agent.OsDesc,
		"AGENT_LISTENER":    agent.Listener,
		"AGENT_INTERNAL_IP": agent.InternalIP,
		"AGENT_EXTERNAL_IP": agent.ExternalIP,
		"AGENT_DOMAIN":      agent.Domain,
		"AGENT_PROCESS":     agent.Process,
	}
}

// renderAgentTemplate substitutes {{AGENT_*}} placeholders.
func renderAgentTemplate(template string, agent adaptix.AgentData) string {
	rendered := template
	for key, value := range agentPlaceholders(agent) {
		rendered = strings.ReplaceAll(rendered, "{{"+key+"}}", value)
	}
	return rendered
}

// buildNotifyPayload renders the built-in push title and Markdown body. It
// deliberately avoids session keys and custom data.
func buildNotifyPayload(agent adaptix.AgentData, prefix string) (string, string) {
	host := agent.Computer
	if host == "" {
		host = agent.InternalIP
	}
	if host == "" {
		host = agent.Id
	}

	title := "AdaptixC2 上线: " + host
	if prefix = strings.TrimSpace(prefix); prefix != "" {
		title = prefix + " " + host
	}

	created := agent.CreateTime
	if created <= 0 {
		created = time.Now().Unix()
	}
	when := time.Unix(created, 0).Format("2006-01-02 15:04:05")

	elevated := "否"
	if agent.Elevated {
		elevated = "是"
	}

	process := agent.Process
	if agent.Pid != "" {
		process = fmt.Sprintf("%s (PID %s)", process, agent.Pid)
	}

	var b strings.Builder
	b.WriteString("### AdaptixC2 新主机上线\n\n")
	b.WriteString("| 字段 | 值 |\n|---|---|\n")
	writeRow(&b, "主机", agent.Computer)
	writeRow(&b, "Agent", fmt.Sprintf("%s (%s)", agent.Name, agent.Id))
	writeRow(&b, "监听器", agent.Listener)
	writeRow(&b, "上线时间", when)
	writeRow(&b, "用户", agent.Username)
	writeRow(&b, "提权", elevated)
	writeRow(&b, "内网 IP", agent.InternalIP)
	writeRow(&b, "外网 IP", agent.ExternalIP)
	writeRow(&b, "系统", agent.OsDesc)
	writeRow(&b, "进程", process)

	return title, b.String()
}

func writeRow(b *strings.Builder, key, value string) {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.TrimSpace(value)
	if value == "" {
		value = "-"
	}
	fmt.Fprintf(b, "| %s | %s |\n", key, value)
}

func truncateRunes(value string, max int) string {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}

// sendNotifyRule delivers the notification action of a rule. It resolves the
// effective channel and template (runtime override beats the profile value),
// renders the message and dispatches it through the channel registry.
func (ts *Teamserver) sendNotifyRule(agent adaptix.AgentData, notify *profile.TsAutoExecNotify, ruleID string) error {
	channelID := ""
	templateID := ""
	prefix := ""

	if notify != nil {
		prefix = notify.Title
		channelID = notify.ChannelID
		templateID = notify.TemplateID
	}

	ts.automationMu.RLock()
	state := ts.automationStore.Rules[ruleID]
	ts.automationMu.RUnlock()

	if state.NotifyChannelID != "" {
		channelID = state.NotifyChannelID
	}
	if state.NotifyTemplateID != "" {
		templateID = state.NotifyTemplateID
	}
	if strings.TrimSpace(channelID) == "" {
		channelID = NotifyChannelServerChan
	}

	title, body := ts.buildNotifyMessage(agent, templateID, prefix)
	return ts.sendThroughChannel(channelID, title, body)
}

// buildNotifyMessage renders a notification template when one is referenced and
// falls back to the built-in payload otherwise.
func (ts *Teamserver) buildNotifyMessage(agent adaptix.AgentData, templateID, prefix string) (string, string) {
	if templateID != "" {
		if tpl, exists := ts.automationNotifyTemplate(templateID); exists {
			title := strings.TrimSpace(tpl.Title)
			body := tpl.Body

			if title == "" {
				title = "AdaptixC2 通知: " + agent.Computer
			}
			title = renderAgentTemplate(title, agent)
			body = renderAgentTemplate(body, agent)

			if body == "" {
				_, body = buildNotifyPayload(agent, "")
			}

			if strings.Contains(title, "{{") || strings.Contains(body, "{{") {
				// An unresolved placeholder means the operator used a key we do
				// not know; fall back rather than sending literal braces.
				return buildNotifyPayload(agent, prefix)
			}
			return title, body
		}
	}

	return buildNotifyPayload(agent, prefix)
}
