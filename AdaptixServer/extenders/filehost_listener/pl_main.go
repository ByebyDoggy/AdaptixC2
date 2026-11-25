package main

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	adaptix "github.com/Adaptix-Framework/axc2"
)

// FileHost listener extender.
//
// It serves a single directory over HTTP with a token path. When an external
// domain is configured, Create() also provisions the Cloudflare side: a CNAME
// record (if missing) and an ingress rule on the existing tunnel that points
// the hostname at this local file server.

type Teamserver interface{}

var (
	ModuleDir       string
	ListenerDataDir string
	Ts              Teamserver
)

func InitPlugin(ts any, moduleDir string, listenerDir string) adaptix.PluginListener {
	ModuleDir = moduleDir
	ListenerDataDir = listenerDir
	return &PluginListener{}
}

type PluginListener struct{}

func (p *PluginListener) Create(name string, config string, customData []byte) (adaptix.ExtenderListener, adaptix.ListenerData, []byte, error) {
	var (
		listener     *Listener
		listenerData adaptix.ListenerData
		conf         TransportConfig
		customdData  []byte
	)

	if customData == nil {
		if err := validConfig(config); err != nil {
			return nil, listenerData, customdData, err
		}

		if err := json.Unmarshal([]byte(config), &conf); err != nil {
			return nil, listenerData, customdData, err
		}

		if conf.PathToken == "" {
			token, err := randomToken(24)
			if err != nil {
				return nil, listenerData, customdData, err
			}
			conf.PathToken = token
		}
		conf.CreatedAt = time.Now().Unix()

		if conf.ExternalDomain != "" {
			if err := provisionCloudflare(&conf, listenerDirOrDefault()); err != nil {
				return nil, listenerData, customdData, err
			}
		}
	} else {
		if err := json.Unmarshal(customData, &conf); err != nil {
			return nil, listenerData, customdData, err
		}
	}

	transport := &TransportFileHost{
		Name:   name,
		Config: conf,
		Active: false,
	}
	listener = &Listener{transport: transport}

	listenerData = adaptix.ListenerData{
		BindHost:  conf.HostBind,
		BindPort:  strconv.Itoa(conf.PortBind),
		AgentAddr: conf.PublicURL(),
		Status:    "Stopped",
	}

	var buffer bytes.Buffer
	if err := json.NewEncoder(&buffer).Encode(transport.Config); err != nil {
		return nil, listenerData, customdData, err
	}
	customdData = buffer.Bytes()

	return listener, listenerData, customdData, nil
}

func listenerDirOrDefault() string {
	if ListenerDataDir != "" {
		return ListenerDataDir
	}
	return ModuleDir
}

type Listener struct {
	transport *TransportFileHost
}

func (l *Listener) Start() error {
	return l.transport.Start()
}

func (l *Listener) Edit(config string) (adaptix.ListenerData, []byte, error) {
	var (
		listenerData adaptix.ListenerData
		conf         TransportConfig
		customdData  []byte
	)

	if err := json.Unmarshal([]byte(config), &conf); err != nil {
		return listenerData, customdData, err
	}

	// Only link-level settings are mutable at runtime; bind address, port and
	// external domain changes require recreating the listener.
	l.transport.mu.Lock()
	if conf.PathToken != "" {
		l.transport.Config.PathToken = conf.PathToken
	}
	l.transport.Config.LinkExpiryMinutes = conf.LinkExpiryMinutes
	l.transport.Config.MaxFileMB = conf.MaxFileMB
	l.transport.Config.IPAllowlist = conf.IPAllowlist
	if conf.PageError != "" {
		l.transport.Config.PageError = conf.PageError
	}
	cfg := l.transport.Config
	l.transport.mu.Unlock()

	listenerData = adaptix.ListenerData{
		BindHost:  cfg.HostBind,
		BindPort:  strconv.Itoa(cfg.PortBind),
		AgentAddr: cfg.PublicURL(),
		Status:    "Listen",
	}
	if !l.transport.Active {
		listenerData.Status = "Closed"
	}

	var buffer bytes.Buffer
	if err := json.NewEncoder(&buffer).Encode(cfg); err != nil {
		return listenerData, customdData, err
	}
	customdData = buffer.Bytes()

	return listenerData, customdData, nil
}

func (l *Listener) Stop() error {
	return l.transport.Stop()
}

func (l *Listener) GetProfile() ([]byte, error) {
	// A file host does not generate agent profiles.
	return nil, nil
}

func (l *Listener) InternalHandler(data []byte) (string, error) {
	return "", nil
}
