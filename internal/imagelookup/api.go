package imagelookup

import (
	"context"
	"github.com/google/go-containerregistry/pkg/authn"
	"net/http"
	"time"
)

// Options supplies transport and command dependencies before a service is used.
// Zero values retain the existing service defaults.
type Options struct {
	Transport                                 http.RoundTripper
	Keychain                                  authn.Keychain
	AllowHTTP                                 bool
	Now                                       func() time.Time
	MaxTagScan                                int
	MaxBuildYAML, MaxBuildLayer, MaxLayerScan int64
	RunCommand                                CommandRunner
}

func NewWithOptions(o Options) *Service {
	s := &Service{transport: o.Transport, keychain: o.Keychain, allowHTTP: o.AllowHTTP, now: o.Now, maxTagScan: o.MaxTagScan, maxBuildYML: o.MaxBuildYAML, maxBuildLayer: o.MaxBuildLayer, maxLayerScan: o.MaxLayerScan, runCommand: o.RunCommand}
	s.defaults()
	return s
}

// WithEnvironmentCredentialsOnly returns an independent service configuration
// for automated public-registry checks. It uses Runway's Docker Hub environment
// credentials when present and anonymous access elsewhere, without reading
// Docker/Podman configuration or running local credential helpers. The original
// service keeps its authentication policy for explicit private-image lookups.
// Transport, limits and command dependencies are retained, never mutated.
func (s *Service) WithEnvironmentCredentialsOnly() *Service {
	if s == nil {
		s = New()
	}
	return NewWithOptions(Options{
		Transport: s.transport, Keychain: imageLookupEnvironmentKeychain{},
		AllowHTTP: s.allowHTTP, Now: s.now, MaxTagScan: s.maxTagScan,
		MaxBuildYAML: s.maxBuildYML, MaxBuildLayer: s.maxBuildLayer,
		MaxLayerScan: s.maxLayerScan, RunCommand: s.runCommand,
	})
}

type Reference struct{ Registry, Repository, Tag, Digest, Canonical string }

func (s *Service) ParseReference(raw string, requireTag bool) (Reference, error) {
	r, err := s.parseReference(raw, requireTag)
	return Reference{Registry: r.registry, Repository: r.repository, Tag: r.tag, Digest: r.digest, Canonical: r.canonical}, err
}
func (s *Service) CloseIdleConnections() { s.closeIdleConnections() }

// SearchMovingHeads keeps the complete moving-head scan used by issue readiness.
func (s *Service) SearchMovingHeads(ctx context.Context, registry, repository string, limit int) SearchGroup {
	return s.searchTargetWithOptions(ctx, searchTarget{registry: registry, repository: repository}, searchOptions{query: "head", limit: limit, fullScan: true, channel: "all", architecture: "all", primeHead: "all", headKind: "all", sortBy: "natural", sortOrder: "desc", movingHeadsOnly: true})
}

// CommandRunner applies the service defaults and returns the configured bounded runner.
func (s *Service) CommandRunner() CommandRunner { s.defaults(); return s.runCommand }
