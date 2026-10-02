// Package server contains transport policy independent of application services.
package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

func RequestFromLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func SameOriginBrowserRequest(r *http.Request) bool {
	if !SameOriginHeaderHost(r.Header.Get("Origin"), r.Host) {
		return SameOriginHeaderHost(r.Header.Get("Referer"), r.Host)
	}
	return true
}

func SameOriginHeaderHost(rawValue, requestHost string) bool {
	rawValue = strings.TrimSpace(rawValue)
	if rawValue == "" {
		return false
	}

	u, err := url.Parse(rawValue)
	if err != nil {
		return false
	}

	return strings.EqualFold(u.Host, requestHost)
}
