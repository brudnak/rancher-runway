package linodeinventory

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSingleDeletionRequiresBothConfirmationsAndFreshIdentity(t *testing.T) {
	for _, scenario := range []string{"success", "missing-first", "missing-second", "wrong-token", "expired", "changed", "attached"} {
		t.Run(scenario, func(t *testing.T) {
			deletes := 0
			label := "atb-node"
			attached := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Error("wrong auth")
				}
				if r.URL.Path != "/volumes/17" {
					t.Error("unexpected target", r.URL.Path)
				}
				if r.Method == "DELETE" {
					deletes++
					fmt.Fprint(w, `{}`)
					return
				}
				linked := "null"
				if attached {
					linked = "3"
				}
				fmt.Fprintf(w, `{"id":17,"label":%q,"linode_id":%s,"created":"2026-10-01"}`, label, linked)
			}))
			defer server.Close()
			s := Service{Client: Client{HTTP: server.Client(), BaseURL: server.URL}}
			plan, err := s.Preview(context.Background(), "test-secret", Ref{"volume", 17})
			if err != nil {
				t.Fatal(err)
			}
			if plan.NameConfirmation != "confirm" || plan.IDConfirmation != "confirm" {
				t.Fatal("both prompts must ask for confirm")
			}
			name, id, token := "confirm", "confirm", "test-secret"
			switch scenario {
			case "missing-first":
				name = ""
			case "missing-second":
				id = ""
			case "wrong-token":
				token = "other-secret"
			case "changed":
				label = "someone-else"
			case "attached":
				attached = true
			case "expired":
				p := s.plans[plan.Token]
				p.ExpiresAt = time.Now().Add(-time.Second)
				s.plans[plan.Token] = p
			}
			_, err = s.Delete(context.Background(), token, plan.Token, name, id)
			if (err == nil) != (scenario == "success") {
				t.Fatal(err)
			}
			expected := 0
			if scenario == "success" {
				expected = 1
			}
			if deletes != expected {
				t.Fatal("unexpected delete count", deletes)
			}
			if scenario == "success" {
				if _, err = s.Delete(context.Background(), token, plan.Token, name, id); err == nil || deletes != 1 {
					t.Fatal("review replayed")
				}
			}
		})
	}
}
func TestConcurrentConfirmSendsOneDelete(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes.Add(1)
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprint(w, `{"id":3,"label":"node"}`)
	}))
	defer server.Close()
	s := Service{Client: Client{HTTP: server.Client(), BaseURL: server.URL}}
	p, err := s.Preview(context.Background(), "token", Ref{"instance", 3})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Delete(context.Background(), "token", p.Token, p.NameConfirmation, p.IDConfirmation)
		}()
	}
	wg.Wait()
	if deletes.Load() != 1 {
		t.Fatal(deletes.Load())
	}
}
func TestInventoryPaginationPartialAccessAndIPv4Formats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/linode/instances":
			if r.URL.Query().Get("page") == "1" {
				fmt.Fprint(w, `{"pages":2,"data":[{"id":1,"label":"other-user-node","ipv4":["192.0.2.1"]}]}`)
			} else {
				fmt.Fprint(w, `{"pages":2,"data":[{"id":2,"label":"atb-node"}]}`)
			}
		case "/nodebalancers":
			fmt.Fprint(w, `{"pages":1,"data":[{"id":4,"label":"shared-balancer","ipv4":"192.0.2.4"}]}`)
		default:
			w.WriteHeader(403)
			fmt.Fprint(w, `{"secret":"DO-NOT-RETURN"}`)
		}
	}))
	defer server.Close()
	inv, err := (Client{HTTP: server.Client(), BaseURL: server.URL}).List(context.Background(), "token")
	if err != nil || len(inv.Items) != 3 || len(inv.Warnings) != 2 || inv.Items[2].IPv4[0] != "192.0.2.4" {
		t.Fatal(inv, err)
	}
	if strings.Contains(strings.Join(inv.Warnings, " "), "DO-NOT-RETURN") {
		t.Fatal("response body leaked")
	}
}
func TestAttachedFirewallAndUnsupportedKindsCannotBeDeleted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Fatal("unexpected write")
		}
		if strings.HasSuffix(r.URL.Path, "/devices") {
			fmt.Fprint(w, `{"data":[{"id":1}],"results":1}`)
		} else {
			fmt.Fprint(w, `{"id":3,"label":"shared-firewall"}`)
		}
	}))
	defer server.Close()
	s := Service{Client: Client{HTTP: server.Client(), BaseURL: server.URL}}
	for _, ref := range []Ref{{"firewall", 3}, {"all", 3}, {"instance", -1}} {
		if _, err := s.Preview(context.Background(), "token", ref); err == nil {
			t.Fatal("unsafe preview", ref)
		}
	}
}
