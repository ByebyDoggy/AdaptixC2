package connector

import (
	"AdaptixServer/core/profile"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// automationChannelsInfo returns the client-visible channel state. The plain
// credential never leaves the teamserver, only the mask and a configured flag.
func (tc *TsConnector) automationChannelsInfo() []profile.AutomationChannelView {
	return tc.teamserver.TsAutomationChannelsList()
}

func (tc *TsConnector) TcAutomationRulesList(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"channels": tc.automationChannelsInfo(),
		"rules":    tc.teamserver.AutomationRulesSnapshot(),
	})
}

func (tc *TsConnector) TcAutomationRulesSet(ctx *gin.Context) {
	var body struct {
		ID      string `json:"id"`
		Enabled *bool  `json:"enabled"`
		Order   *int   `json:"order"`
	}

	if err := ctx.ShouldBindJSON(&body); err != nil || body.ID == "" {
		ctx.JSON(http.StatusOK, gin.H{
			"ok":      false,
			"message": "id is required",
			"rules":   tc.teamserver.AutomationRulesSnapshot(),
		})
		return
	}

	if body.Enabled != nil {
		if err := tc.teamserver.SetAutomationRuleEnabled(body.ID, *body.Enabled); err != nil {
			ctx.JSON(http.StatusOK, gin.H{
				"ok":      false,
				"message": err.Error(),
				"rules":   tc.teamserver.AutomationRulesSnapshot(),
			})
			return
		}
	}

	if body.Order != nil {
		if err := tc.teamserver.SetAutomationRuleOrder(body.ID, *body.Order); err != nil {
			ctx.JSON(http.StatusOK, gin.H{
				"ok":      false,
				"message": err.Error(),
				"rules":   tc.teamserver.AutomationRulesSnapshot(),
			})
			return
		}
	}

	if body.Enabled == nil && body.Order == nil {
		ctx.JSON(http.StatusOK, gin.H{
			"ok":      false,
			"message": "enabled or order is required",
			"rules":   tc.teamserver.AutomationRulesSnapshot(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"message":  "automation rule updated",
		"rules":    tc.teamserver.AutomationRulesSnapshot(),
		"channels": tc.automationChannelsInfo(),
	})
}

// TcAutomationRuleUpdate applies a full rule configuration patch.
func (tc *TsConnector) TcAutomationRuleUpdate(ctx *gin.Context) {
	var body map[string]any

	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": "invalid request"})
		return
	}

	ruleID, _ := body["id"].(string)
	if strings.TrimSpace(ruleID) == "" {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": "id is required"})
		return
	}
	delete(body, "id")

	if err := tc.teamserver.AutomationRuleUpdate(ruleID, body); err != nil {
		ctx.JSON(http.StatusOK, gin.H{
			"ok":      false,
			"message": err.Error(),
			"rules":   tc.teamserver.AutomationRulesSnapshot(),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"message":  "rule configuration updated",
		"rules":    tc.teamserver.AutomationRulesSnapshot(),
		"channels": tc.automationChannelsInfo(),
	})
}

func (tc *TsConnector) TcAutomationTemplatesList(ctx *gin.Context) {
	var body struct {
		Kind string `json:"kind"`
	}

	// An empty body lists both kinds.
	_ = ctx.ShouldBindJSON(&body)
	kind := strings.TrimSpace(body.Kind)

	if kind != "" {
		ctx.JSON(http.StatusOK, gin.H{
			"ok":        true,
			"kind":      kind,
			"templates": tc.teamserver.TsAutomationTemplatesList(kind),
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"artex":  tc.teamserver.TsAutomationTemplatesList("artex"),
		"notify": tc.teamserver.TsAutomationTemplatesList("notify"),
	})
}

func (tc *TsConnector) TcAutomationTemplateGet(ctx *gin.Context) {
	var body struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}

	if err := ctx.ShouldBindJSON(&body); err != nil || body.Kind == "" || body.ID == "" {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": "kind and id are required"})
		return
	}

	template, err := tc.teamserver.TsAutomationTemplateGet(body.Kind, body.ID)
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"ok": true, "template": template})
}

func (tc *TsConnector) TcAutomationTemplateSet(ctx *gin.Context) {
	var body struct {
		Kind     string `json:"kind"`
		ID       string `json:"id"`
		Template struct {
			Name            string `json:"name"`
			Description     string `json:"description"`
			Goal            string `json:"goal"`
			Title           string `json:"title"`
			Body            string `json:"body"`
			WorkerCount     int    `json:"worker_count"`
			TimeoutSeconds  int    `json:"timeout_seconds"`
			SeedFirstIntent bool   `json:"seed_first_intent"`
			DedupBy         string `json:"dedup_by"`
			CooldownSeconds int    `json:"cooldown_seconds"`
			SkipIfRunning   bool   `json:"skip_if_running"`
		} `json:"template"`
	}

	if err := ctx.ShouldBindJSON(&body); err != nil || body.Kind == "" || body.ID == "" {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": "kind and id are required"})
		return
	}

	view := profile.AutomationTemplateView{
		ID:              body.ID,
		Kind:            body.Kind,
		Name:            body.Template.Name,
		Description:     body.Template.Description,
		Goal:            body.Template.Goal,
		Title:           body.Template.Title,
		Body:            body.Template.Body,
		WorkerCount:     body.Template.WorkerCount,
		TimeoutSeconds:  body.Template.TimeoutSeconds,
		SeedFirstIntent: body.Template.SeedFirstIntent,
		DedupBy:         body.Template.DedupBy,
		CooldownSeconds: body.Template.CooldownSeconds,
		SkipIfRunning:   body.Template.SkipIfRunning,
	}

	if err := tc.teamserver.TsAutomationTemplateSet(body.Kind, body.ID, view); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}

	template, _ := tc.teamserver.TsAutomationTemplateGet(body.Kind, body.ID)

	ctx.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"message":  "template saved",
		"template": template,
	})
}

func (tc *TsConnector) TcAutomationTemplateDelete(ctx *gin.Context) {
	var body struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}

	if err := ctx.ShouldBindJSON(&body); err != nil || body.Kind == "" || body.ID == "" {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": "kind and id are required"})
		return
	}

	if err := tc.teamserver.TsAutomationTemplateDelete(body.Kind, body.ID); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "template deleted",
		"kind":    body.Kind,
		"id":      body.ID,
	})
}

func (tc *TsConnector) TcAutomationChannelsList(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"channels": tc.automationChannelsInfo(),
	})
}

func (tc *TsConnector) TcAutomationChannelSet(ctx *gin.Context) {
	var body struct {
		ID      string `json:"id"`
		SendKey string `json:"send_key"`
		APIBase string `json:"api_base"`
	}

	if err := ctx.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.ID) == "" {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": "id is required"})
		return
	}

	if err := tc.teamserver.TsAutomationChannelSet(body.ID, body.SendKey, body.APIBase); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"message":  "channel configuration updated",
		"channels": tc.automationChannelsInfo(),
	})
}

func (tc *TsConnector) TcAutomationScriptsList(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"scripts": tc.teamserver.TsScriptsList(),
	})
}

func (tc *TsConnector) TcAutomationScriptsRun(ctx *gin.Context) {
	var body struct {
		AgentID  string   `json:"agent_id"`
		ScriptID string   `json:"script_id"`
		Args     []string `json:"args"`
	}

	if err := ctx.ShouldBindJSON(&body); err != nil || body.AgentID == "" || body.ScriptID == "" {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": "agent_id and script_id are required"})
		return
	}

	args := make([]string, 0, len(body.Args))
	for _, arg := range body.Args {
		arg = strings.TrimSpace(arg)
		if arg != "" {
			args = append(args, arg)
		}
	}

	if err := tc.teamserver.TsScriptRun(body.AgentID, body.ScriptID, args); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":        true,
		"message":   "script started",
		"script_id": body.ScriptID,
		"agent_id":  body.AgentID,
	})
}

func (tc *TsConnector) TcAutomationKeysInfo(ctx *gin.Context) {
	view, err := tc.teamserver.TsPersistKeyInfo()
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "key": view})
}

func (tc *TsConnector) TcAutomationKeysPrivate(ctx *gin.Context) {
	privateKey, err := tc.teamserver.TsPersistKeyPrivate()
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{"ok": true, "private_key": privateKey})
}
