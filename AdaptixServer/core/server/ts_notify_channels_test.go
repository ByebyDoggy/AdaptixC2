package server

import (
	"AdaptixServer/core/profile"
	"strings"
	"testing"
)

func TestNotifyChannelRegistryHasServerChan(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	channel, exists := ts.NotifyChannel(NotifyChannelServerChan)
	if !exists {
		t.Fatal("serverchan channel must be registered")
	}
	if channel.ID() != NotifyChannelServerChan {
		t.Fatalf("id = %q", channel.ID())
	}
	if channel.Name() == "" || channel.SecretLabel() == "" {
		t.Fatal("channel metadata must be populated")
	}
}

func TestNotifyChannelUnknown(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if _, exists := ts.NotifyChannel("nope"); exists {
		t.Fatal("unknown channel must not resolve")
	}
}

func TestSendThroughChannelRequiresConfiguration(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	err := ts.sendThroughChannel(NotifyChannelServerChan, "title", "body")
	if err == nil {
		t.Fatal("expected unconfigured channel to fail")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("error = %v", err)
	}
}

func TestSendThroughChannelUnknownChannel(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	err := ts.sendThroughChannel("nope", "title", "body")
	if err == nil || !strings.Contains(err.Error(), "unknown notification channel") {
		t.Fatalf("error = %v", err)
	}
}

func TestAutomationChannelsListMasksSecret(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if err := ts.TsAutomationChannelSet(NotifyChannelServerChan, "SCT428286TSTjgCuVK99P276JPHwimCfQR", ""); err != nil {
		t.Fatalf("set channel: %v", err)
	}

	channels := ts.TsAutomationChannelsList()
	if len(channels) != 1 {
		t.Fatalf("channels = %d", len(channels))
	}

	view := channels[0]
	if !view.Configured {
		t.Fatal("channel should be configured")
	}
	if view.Masked != "SCT4...CfQR" {
		t.Fatalf("masked = %q", view.Masked)
	}
	if strings.Contains(view.Masked, "TSTjgCuVK99P276JPHwim") {
		t.Fatal("mask must not reveal the middle of the credential")
	}
}

func TestAutomationChannelSetRejectsUnknown(t *testing.T) {
	ts, _ := newAutomationTestTeamserver(t, []profile.TsAutoExecRule{{ID: "rule-a"}})

	if err := ts.TsAutomationChannelSet("nope", "secret", ""); err == nil {
		t.Fatal("expected unknown channel to be rejected")
	}
}
