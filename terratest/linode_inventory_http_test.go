package test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLinodeInventoryHTTPRejectsBulkAndUnauthenticatedRequests(t *testing.T) {
	p := &localControlPanel{token: "test-token"}
	for _, test := range []struct {
		body       string
		authorized bool
		status     int
	}{
		{`{"action":"delete"}`, false, 403},
		{`{"action":"delete","resources":[{"type":"instance","id":1}]}`, true, 400},
		{`{"action":"preview","resource":[{"type":"instance","id":1}]}`, true, 400},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/linode/inventory", bytes.NewBufferString(test.body))
		if test.authorized {
			request.Header.Set("X-Control-Panel-Token", "test-token")
		}
		w := httptest.NewRecorder()
		p.handleLinodeInventory(w, request)
		if w.Code != test.status {
			t.Fatalf("%s: %d %s", test.body, w.Code, w.Body.String())
		}
	}
}
