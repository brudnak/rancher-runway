package imagelookup

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"
)

func (s *Service) remoteOptions(ctx context.Context, platform *v1.Platform, pageSize int) []remote.Option {
	options := []remote.Option{
		remote.WithContext(ctx),
		remote.WithTransport(s.transport),
		remote.WithAuthFromKeychain(s.keychain),
		remote.WithUserAgent("rancher-runway-image-lookup"),
	}
	if platform != nil {
		options = append(options, remote.WithPlatform(*platform))
	}
	if pageSize > 0 {
		options = append(options, remote.WithPageSize(pageSize))
	}
	return options
}

func imageLookupExactTagReference(repository name.Repository, query string, allowHTTP bool) (name.Tag, bool) {
	query = strings.TrimSpace(query)
	if query == "" || imageLookupQuickQuery(query) || strings.EqualFold(query, "all") {
		return name.Tag{}, false
	}
	options := []name.Option{name.StrictValidation}
	if allowHTTP {
		options = append(options, name.Insecure)
	}
	tag, err := name.NewTag(repository.Name()+":"+query, options...)
	return tag, err == nil
}

func imageLookupExactDigestReference(repository name.Repository, query string, allowHTTP bool) (name.Digest, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	if !imageLookupDigestPattern.MatchString(query) {
		return name.Digest{}, false
	}
	options := []name.Option{name.StrictValidation}
	if allowHTTP {
		options = append(options, name.Insecure)
	}
	digest, err := name.NewDigest(repository.Name()+"@"+query, options...)
	return digest, err == nil
}

func imageLookupFullVersionTag(query string) bool {
	return imageLookupFullVersionTagPattern.MatchString(strings.TrimSpace(query))
}

type imageLookupCredentialKeychain struct{}

func (imageLookupCredentialKeychain) Resolve(resource authn.Resource) (authn.Authenticator, error) {
	registry := RegistryForDisplay(resource.RegistryStr())
	username := strings.TrimSpace(os.Getenv("DOCKERHUB_USERNAME"))
	password := os.Getenv("DOCKERHUB_PASSWORD")
	if registry == "docker.io" && username != "" && password != "" {
		return authn.FromConfig(authn.AuthConfig{Username: username, Password: password}), nil
	}
	return authn.DefaultKeychain.Resolve(resource)
}

type imageLookupSafeRoundTripper struct {
	inner http.RoundTripper
}

func (t *imageLookupSafeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, errors.New("registry request has no URL")
	}
	if request.URL.Scheme != "https" {
		return nil, fmt.Errorf("registry request scheme %q is not allowed", request.URL.Scheme)
	}
	if request.URL.User != nil || request.URL.Hostname() == "" {
		return nil, errors.New("registry request authority is invalid")
	}
	return t.inner.RoundTrip(request)
}

func (t *imageLookupSafeRoundTripper) CloseIdleConnections() {
	if closer, ok := t.inner.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (s *Service) closeIdleConnections() {
	if s == nil {
		return
	}
	if closer, ok := s.transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func newImageLookupSafeTransport() http.RoundTripper {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	base := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid registry address: %w", err)
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve registry host: %w", err)
		}
		if len(addresses) == 0 {
			return nil, errors.New("registry host did not resolve")
		}
		for _, candidate := range addresses {
			if !imageLookupPublicIP(candidate) {
				return nil, fmt.Errorf("registry host resolves to a private or reserved address")
			}
		}
		var lastErr error
		for _, candidate := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
	return &imageLookupSafeRoundTripper{inner: base}
}

var imageLookupBlockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func imageLookupPublicIP(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range imageLookupBlockedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
