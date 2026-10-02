package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeployedMachineSchemaFormats(t *testing.T) {
	for _, tc := range []struct{ name, schema, definition, wantError string }{
		{"inline", `{"resourceFields":{"region":{"type":"string","create":true},"metadata":{"create":true},"immutable":{"create":false}}}`, "", ""},
		{"Steve definitions", `{"resourceFields":null,"collectionMethods":["GET","POST"]}`, `{"definitionType":"machine","definitions":{"machine":{"resourceFields":{"region":{"type":"string"},"enabled":{"type":"boolean"},"securityGroup":{"type":"array","subtype":"string"},"metadata":{"type":"object"},"secretKey":{"type":"string"},"sshKeyContents":{"type":"string"},"readonly":{"type":"string","readOnly":true}}},"unrelated":{"resourceFields":{"wrong":{"type":"string"}}}}}`, ""},
		{"read-only account", `{"resourceFields":null,"collectionMethods":["GET"]}`, "", "cannot create"},
		{"future schema", `{"fields":{"region":{}}}`, "", "does not recognize"},
		{"future definition", `{"collectionMethods":["POST"]}`, `{"definitions":{"unknown":{}}}`, "root definition is missing"},
		{"not published yet", `{"collectionMethods":["POST"]}`, `{"definitionType":"machine","definitions":{"machine":{"resourceFields":{}}}}`, "not ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields, err := deployedMachineFields("amazonec2", func(path string, out any) error {
				raw := tc.schema
				if strings.Contains(path, "/schemaDefinitions/") {
					raw = tc.definition
				}
				if raw == "" {
					t.Fatal("unexpected definitions lookup")
				}
				return json.Unmarshal([]byte(raw), out)
			})
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected %s, got %v", tc.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fields["region"].Type != "string" {
				t.Fatal("region missing")
			}
			for _, key := range []string{"metadata", "secretKey", "readonly", "immutable", "wrong"} {
				if _, ok := fields[key]; ok {
					t.Fatalf("unexpected field %s", key)
				}
			}
			if tc.name == "Steve definitions" && (fields["sshKeyContents"].Type != "password" || fields["securityGroup"].Type != "array") {
				t.Fatal("lost machine field types")
			}
		})
	}
}

func TestDeployedDriverActivation(t *testing.T) {
	for _, scenario := range []string{"inactive", "already-active", "denied", "timeout", "unknown-format"} {
		t.Run(scenario, func(t *testing.T) {
			activations := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture-token" {
					t.Error("missing auth")
				}
				switch r.URL.RequestURI() {
				case "/v3/nodeDrivers/linode":
					fmt.Fprintf(w, `{"active":%t}`, scenario == "already-active")
				case "/v3/nodeDrivers/linode?action=activate":
					if r.Method != "POST" {
						t.Error("activation method")
					}
					activations++
					if scenario == "denied" {
						http.Error(w, "private error", 403)
						return
					}
					w.WriteHeader(204)
				case "/v1/schemas/rke-machine-config.cattle.io.linodeconfig":
					if scenario == "timeout" {
						w.WriteHeader(404)
						return
					}
					fmt.Fprint(w, `{"resourceFields":null,"collectionMethods":["GET","POST"]}`)
				case "/v1/schemaDefinitions/rke-machine-config.cattle.io.linodeconfig":
					if scenario == "unknown-format" {
						fmt.Fprint(w, `{}`)
						return
					}
					fmt.Fprint(w, `{"definitionType":"linode","definitions":{"linode":{"resourceFields":{"region":{"type":"string"}}}}}`)
				default:
					t.Errorf("unexpected action %s %s", r.Method, r.URL.RequestURI())
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			in := rancherOperationRequest{Provider: "linode", Token: "fixture-token", Insecure: true}
			cluster := clusterView{RancherURL: server.URL}
			state, err := deployedDriverStatus(context.Background(), cluster, in)
			if err != nil || state.Active == nil || activations != 0 {
				t.Fatal("discovery mutated the driver or failed")
			}
			ctx := context.Background()
			if scenario == "timeout" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				defer cancel()
			}
			_, err = enableDeployedDriver(ctx, cluster, in)
			succeeds := scenario == "inactive" || scenario == "already-active"
			if succeeds != (err == nil) {
				t.Fatalf("unexpected activation result: %v", err)
			}
			expected := 1
			if scenario == "already-active" {
				expected = 0
			}
			if activations != expected {
				t.Fatalf("activation count %d", activations)
			}
			if err != nil && strings.Contains(err.Error(), "private error") {
				t.Fatal("leaked response")
			}
		})
	}
	if _, err := enableDeployedDriver(context.Background(), clusterView{}, rancherOperationRequest{Provider: "../custom"}); err == nil {
		t.Fatal("accepted arbitrary driver")
	}
}

func TestDeployedDriverSchemaDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		status       int
		driver, want string
	}{
		{404, `{"active":false}`, "is inactive"}, {404, `{"active":true}`, "is enabled"}, {404, `{}`, "could not verify"}, {403, "", "permissions"}, {401, "", "Sign in again"}, {500, "", "HTTP 500"},
	} {
		err := deployedDriverSchemaError("linode", &deployedRancherHTTPError{"GET", "/schema", tc.status}, func(path string, out any) error {
			if tc.driver == "" {
				t.Fatal("unnecessary driver lookup")
			}
			return json.Unmarshal([]byte(tc.driver), out)
		})
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v", err)
		}
	}
}
