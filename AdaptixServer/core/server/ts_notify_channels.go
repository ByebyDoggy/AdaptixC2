package server

import (
	"errors"
	"fmt"
	"strings"
)

// NotifyChannelServerChan is the id of the built-in ServerChan channel.
const NotifyChannelServerChan = "serverchan"

// NotifyChannel is one outbound notification transport. Implementations hold
// no credential themselves: they read it from the teamserver's runtime channel
// store so credential rotation needs no re-registration.
//
// Adding a channel means implementing this interface and registering it in
// newNotifyChannelRegistry; the client needs no change because it only renders
// the view returned by TsAutomationChannelsList.
type NotifyChannel interface {
	// ID is the stable key used by rules and by the runtime store.
	ID() string
	// Name is the display name.
	Name() string
	// SecretLabel describes what the credential is called in the UI.
	SecretLabel() string
	// Configured reports whether a credential is present.
	Configured() bool
	// Masked renders the credential for display, never in clear.
	Masked() string
	// Send delivers one message.
	Send(title, body string) error
}

type notifyChannelRegistry struct {
	channels map[string]NotifyChannel
	order    []string
}

func newNotifyChannelRegistry(ts *Teamserver) *notifyChannelRegistry {
	registry := &notifyChannelRegistry{channels: make(map[string]NotifyChannel)}

	registry.register(&serverChanChannel{ts: ts})

	return registry
}

func (r *notifyChannelRegistry) register(channel NotifyChannel) {
	if _, exists := r.channels[channel.ID()]; exists {
		return
	}
	r.channels[channel.ID()] = channel
	r.order = append(r.order, channel.ID())
}

// ensureNotifyChannels lazily builds the registry so a Teamserver constructed
// outside NewTeamserver (notably in tests) still has the built-in channels.
func (ts *Teamserver) ensureNotifyChannels() *notifyChannelRegistry {
	ts.automationMu.Lock()
	defer ts.automationMu.Unlock()

	if ts.notifyChannelsRegistry == nil {
		ts.notifyChannelsRegistry = newNotifyChannelRegistry(ts)
	}
	return ts.notifyChannelsRegistry
}

// NotifyChannel resolves a channel by id.
func (ts *Teamserver) NotifyChannel(id string) (NotifyChannel, bool) {
	registry := ts.ensureNotifyChannels()
	channel, exists := registry.channels[id]
	return channel, exists
}

// notifyChannels returns every registered channel in registration order.
func (ts *Teamserver) notifyChannels() []NotifyChannel {
	registry := ts.ensureNotifyChannels()
	result := make([]NotifyChannel, 0, len(registry.order))
	for _, id := range registry.order {
		result = append(result, registry.channels[id])
	}
	return result
}

// sendThroughChannel delivers a notification through the named channel.
func (ts *Teamserver) sendThroughChannel(channelID, title, body string) error {
	if strings.TrimSpace(channelID) == "" {
		channelID = NotifyChannelServerChan
	}
	channel, exists := ts.NotifyChannel(channelID)
	if !exists {
		return fmt.Errorf("unknown notification channel '%s'", channelID)
	}
	if !channel.Configured() {
		return fmt.Errorf("notification channel '%s' is not configured", channelID)
	}
	return channel.Send(title, body)
}

// --- ServerChan ---------------------------------------------------------------

// serverChanChannel implements NotifyChannel on top of the ServerChan HTTP API.
type serverChanChannel struct {
	ts *Teamserver
}

func (c *serverChanChannel) ID() string { return NotifyChannelServerChan }

func (c *serverChanChannel) Name() string { return "ServerChan" }

func (c *serverChanChannel) SecretLabel() string { return "SendKey" }

func (c *serverChanChannel) Configured() bool {
	return strings.TrimSpace(c.ts.AutomationChannelSendKey(NotifyChannelServerChan)) != ""
}

func (c *serverChanChannel) Masked() string {
	return maskSecret(c.ts.AutomationChannelSendKey(NotifyChannelServerChan))
}

func (c *serverChanChannel) Send(title, body string) error {
	sendKey := c.ts.AutomationChannelSendKey(NotifyChannelServerChan)
	if strings.TrimSpace(sendKey) == "" {
		return errors.New("serverchan send_key is empty")
	}
	return sendServerChan(&serverChanConfig{
		SendKey: sendKey,
		APIBase: c.ts.AutomationChannelAPIBase(NotifyChannelServerChan),
	}, title, body)
}
