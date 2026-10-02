package imagelookup

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestImageLookupSafeTransportRejectsPrivateAddresses(t *testing.T) {
	for _, address := range []string{
		"0.0.0.0",
		"10.0.0.1",
		"100.64.0.1",
		"127.0.0.1",
		"169.254.169.254",
		"172.16.0.1",
		"192.168.1.1",
		"::1",
		"fc00::1",
		"fe80::1",
		"2001:db8::1",
		"::ffff:127.0.0.1",
	} {
		if imageLookupPublicIP(netip.MustParseAddr(address)) {
			t.Errorf("private or reserved address %s was allowed", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !imageLookupPublicIP(netip.MustParseAddr(address)) {
			t.Errorf("public address %s was blocked", address)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "https://127.0.0.1/v2/", nil)
	_, err := newImageLookupSafeTransport().RoundTrip(request)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "private or reserved") {
		t.Fatalf("safe transport error = %v, want private/reserved rejection", err)
	}
}

func TestImageLookupServiceClosesWrappedIdleConnections(t *testing.T) {
	inner := &imageLookupCloseTrackingRoundTripper{}
	service := &Service{
		transport: &imageLookupSafeRoundTripper{inner: inner},
	}

	service.closeIdleConnections()

	if got := inner.closes.Load(); got != 1 {
		t.Fatalf("idle connection close calls = %d, want 1", got)
	}
}
