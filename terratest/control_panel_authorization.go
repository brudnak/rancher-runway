package test

import (
	"net/http"
	"strings"

	"github.com/brudnak/ha-rancher-rke2/internal/server"
)

func (p *localControlPanel) authorized(r *http.Request) bool {
	if p.trustedLocalOrigin {
		return true
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-Control-Panel-Token"))
	}
	return token != "" && token == p.token
}

func (p *localControlPanel) authorizedReadOnly(r *http.Request) bool {
	return p.authorized(r) || requestFromLoopback(r)
}

func (p *localControlPanel) authorizedLocalBrowserRead(r *http.Request) bool {
	return p.authorized(r) || (requestFromLoopback(r) && sameOriginBrowserRequest(r))
}

func (p *localControlPanel) authorizedLocalAction(r *http.Request) bool {
	return p.authorized(r) || (requestFromLoopback(r) && sameOriginBrowserRequest(r))
}

func requestFromLoopback(r *http.Request) bool { return server.RequestFromLoopback(r) }

func sameOriginBrowserRequest(r *http.Request) bool { return server.SameOriginBrowserRequest(r) }

func sameOriginHeaderHost(value, host string) bool { return server.SameOriginHeaderHost(value, host) }
