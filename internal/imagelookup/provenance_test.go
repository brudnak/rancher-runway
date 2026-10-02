package imagelookup

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestInspectProvenancePreservesRuntimeVersionAndBranchLabel(t *testing.T) {
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer server.Close()
	image := newImageLookupFixtureImage(t, "amd64", "", time.Now(), nil)
	config, err := image.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	// These fields reproduce the public v2.15-head config: its OCI version is
	// a branch label, while the runtime version is a commit-qualified build.
	runtimeVersion := "v2.15-19c92983f6f9d7f455de668e62fbfe55c045cde2-head"
	config.Config.Labels = map[string]string{VersionLabel: "release-v2.15", CanonicalReferenceLabel: "rancher/rancher:" + runtimeVersion}
	config.Config.Env = []string{"CATTLE_SERVER_VERSION=" + runtimeVersion, "UNRELATED=not-exported"}
	image, err = mutate.ConfigFile(image, config)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.NewTag(imageLookupTestServerHost(t, server)+"/rancher/rancher:v2.15-head", name.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	if err = remote.Write(ref, image, remote.WithAuth(authn.Anonymous), remote.WithTransport(server.Client().Transport)); err != nil {
		t.Fatal(err)
	}
	got, found, err := InspectProvenance(context.Background(), newImageLookupTestService(t, server), ref.Name())
	if err != nil || !found || got.ServerVersion != runtimeVersion || got.BuildVersion != "release-v2.15" || got.Digest == "" {
		t.Fatalf("provenance: %+v found=%v err=%v", got, found, err)
	}
}
