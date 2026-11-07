package test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestLinodeInventoryDeleteBlocksDuringProvisioningHandoff(t *testing.T) {
	p := &localControlPanel{
		token: "test-token",
		operations: map[panelOperationName]*panelOperationState{
			panelOperationForRun(panelOperationSetup, "prime"): {RunID: "prime", Completing: true},
		},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/linode/inventory", strings.NewReader(`{"action":"delete","token":"test-token"}`))
	request.Header.Set("X-Control-Panel-Token", "test-token")
	w := httptest.NewRecorder()
	p.handleLinodeInventory(w, request)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "active Runway operation") {
		t.Fatalf("delete was not blocked during provisioning handoff: %d %s", w.Code, w.Body.String())
	}
	if len(p.rancherOps.active) != 0 {
		t.Fatalf("blocked deletion reserved an operation: %v", p.rancherOps.active)
	}
}
