package test

import (
	"strings"
	"testing"
)

func TestRedactDiagnosticOutput(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	cases := []struct{ name, input, want string }{
		{"kubelet", "--hostname-override=ip-172-31-13-235 --node-ip=***,2600:1f16:1d38:1c00:f24e:b304:a158:ad9a --read-only-port=0", "--hostname-override=[REDACTED_HOST] --node-ip=***,[REDACTED_IP] --read-only-port=0"},
		{"TLS annotation", "listener.cattle.io/cn-2600:1f16:1d38:1c00:f24e:b304:a158:ad9a:2600:1f16:1d38:1c00:f24e:b304:a158:ad9a", "listener.cattle.io/cn-[REDACTED_IP]:[REDACTED_IP]"},
		{"IPv4", "node 172.31.13.235 pod 10.42.0.9", "node [REDACTED_IP] pod [REDACTED_IP]"},
		{"compressed IPv6", "https://[2001:db8::1]:9345 ::1 ::", "https://[[REDACTED_IP]]:9345 [REDACTED_IP] [REDACTED_IP]"},
		{"diagnostic details", "Sep 30 22:08:06 CPU: 58.422s v1.32.13 sha256:abcdef0123456789 running", "Sep 30 22:08:06 CPU: 58.422s v1.32.13 sha256:abcdef0123456789 running"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := redactDiagnosticOutput(c.input); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

func TestRedactDiagnosticOutputLocal(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "false")
	input := "ip-172-31-13-235 --node-ip=172.31.13.235,2001:db8::1"
	if got := redactDiagnosticOutput(input); !strings.EqualFold(got, input) {
		t.Fatalf("local output changed: %q", got)
	}
}
