package routes

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/TrueFix/httptoy/pkg/httpx"
)

func connectionInfo(w http.ResponseWriter, req *http.Request) {
	var tlsInfo any
	if req.TLS != nil {
		tlsInfo = map[string]any{"version": tls.VersionName(req.TLS.Version), "cipher_suite": tls.CipherSuiteName(req.TLS.CipherSuite),
			"alpn": req.TLS.NegotiatedProtocol, "server_name": req.TLS.ServerName, "handshake_complete": req.TLS.HandshakeComplete}
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"http_version": req.Proto, "peer_address": req.RemoteAddr, "client_ip": httpx.ClientIP(req), "tls": tlsInfo})
}

func serverTime(w http.ResponseWriter, req *http.Request) {
	now := time.Now().UTC()
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"rfc3339": now.Format(time.RFC3339Nano), "unix": now.Unix(), "unix_ms": now.UnixMilli()})
}
