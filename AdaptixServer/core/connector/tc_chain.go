package connector

import (
	"AdaptixServer/core/profile"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (tc *TsConnector) TcChainConfigGet(ctx *gin.Context) {
	ctx.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"config": tc.teamserver.TsChainConfigGet(),
	})
}

func (tc *TsConnector) TcChainConfigSet(ctx *gin.Context) {
	var cfg profile.TsChainConfig
	if err := ctx.ShouldBindJSON(&cfg); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": "invalid chain config"})
		return
	}

	if err := tc.teamserver.TsChainConfigSet(cfg); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "chain config saved",
		"config":  tc.teamserver.TsChainConfigGet(),
	})
}

func (tc *TsConnector) TcChainStatus(ctx *gin.Context) {
	cfg := tc.teamserver.TsChainConfigGet()

	status, err := tc.teamserver.TsChainStatus()
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{
			"ok":      false,
			"message": err.Error(),
			"config":  cfg,
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"status": status,
		"config": cfg,
	})
}

func (tc *TsConnector) TcChainCalldata(ctx *gin.Context) {
	var body struct {
		Domain string `json:"domain"`
	}
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": "invalid request"})
		return
	}

	calldata, err := tc.teamserver.TsChainCalldata(body.Domain)
	if err != nil {
		ctx.JSON(http.StatusOK, gin.H{"ok": false, "message": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"ok":       true,
		"calldata": calldata,
	})
}
