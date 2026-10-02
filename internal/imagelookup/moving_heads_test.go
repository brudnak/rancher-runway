package imagelookup

import (
	"context"
	"encoding/json"
	"fmt"

	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadinessMovingHeadsFilterBeforeLimit(t *testing.T) {
	tags := []string{}
	for i := 0; i < 150; i++ {
		tags = append(tags, fmt.Sprintf("v2.99.0-head-%04d", i))
	}
	tags = append(tags, "head", "v2.14-head", "v2.14.1-head")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tags/list") {
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "rancher/rancher", "tags": tags})
			return
		}
		if r.URL.Path == "/v2/" {
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	service := NewWithOptions(Options{Transport: server.Client().Transport, Keychain: imageLookupAnonymousTestKeychain{}, AllowHTTP: true, MaxTagScan: 1000})
	got := service.SearchMovingHeads(context.Background(), imageLookupTestServerHost(t, server), "rancher/rancher", 3)
	if got.Error != "" || len(got.Tags) != 3 {
		t.Fatalf("moving heads=%+v", got)
	}
	for _, tag := range got.Tags {
		if !ReadinessHeadPattern.MatchString(tag.Name) {
			t.Fatalf("immutable tag displaced moving head: %s", tag.Name)
		}
	}
}
