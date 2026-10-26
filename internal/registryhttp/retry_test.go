package registryhttp

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func response(req *http.Request, status int, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: body, Request: req}
}

func request(t *testing.T, method string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, "https://registry.example.test/v2/rancher/rancher-webhook/manifests/v0.7.13-rc.1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	return req
}

func TestDoRecoversFromClientTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		client := &http.Client{Timeout: 30 * time.Second, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method != http.MethodHead || req.Header.Get("Authorization") != "Bearer test-token" || req.Header.Get("Accept") == "" {
				t.Errorf("retry changed registry request: %v", req)
			}
			if calls == 1 {
				<-req.Context().Done()
				return nil, req.Context().Err()
			}
			return response(req, http.StatusOK, http.NoBody), nil
		})}
		started := time.Now()
		resp, err := Do(client, request(t, http.MethodHead))
		if err != nil || resp.StatusCode != http.StatusOK || calls != 2 {
			t.Fatalf("response=%v error=%v calls=%d", resp, err, calls)
		}
		resp.Body.Close()
		if elapsed := time.Since(started); elapsed != 31*time.Second {
			t.Fatalf("elapsed=%s, want 30s timeout plus 1s backoff", elapsed)
		}
	})
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func TestDoRetriesTemporaryHTTPFailures(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 504} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				failedBody := &trackedBody{Reader: strings.NewReader("registry temporarily unavailable")}
				client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return response(req, status, failedBody), nil
					}
					if !failedBody.closed {
						t.Error("previous response body is still open")
					}
					return response(req, http.StatusOK, io.NopCloser(strings.NewReader(`{"token":"token"}`))), nil
				})}
				resp, err := Do(client, request(t, http.MethodGet))
				if err != nil || resp.StatusCode != http.StatusOK || calls != 2 {
					t.Fatalf("response=%v error=%v calls=%d", resp, err, calls)
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil || string(body) != `{"token":"token"}` {
					t.Fatalf("final response body=%q error=%v", body, err)
				}
			})
		})
	}
}

func TestDoExhaustsRetriesAndPreservesFinalResponse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		var bodies []*trackedBody
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			body := &trackedBody{Reader: strings.NewReader("unavailable")}
			bodies = append(bodies, body)
			return response(req, http.StatusServiceUnavailable, body), nil
		})}
		started := time.Now()
		resp, err := Do(client, request(t, http.MethodHead))
		if err != nil || resp.StatusCode != http.StatusServiceUnavailable || calls != 3 || time.Since(started) != 3*time.Second {
			t.Fatalf("response=%v error=%v calls=%d elapsed=%s", resp, err, calls, time.Since(started))
		}
		defer resp.Body.Close()
		if !bodies[0].closed || !bodies[1].closed || bodies[2].closed {
			t.Fatalf("unexpected response body ownership: %v", bodies)
		}
	})
}

func TestDoClassifiesTransportErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"connection reset", syscall.ECONNRESET, 3},
		{"broken pipe", syscall.EPIPE, 3},
		{"EOF", io.EOF, 3},
		{"unexpected EOF", io.ErrUnexpectedEOF, 3},
		{"timeout", context.DeadlineExceeded, 3},
		{"temporary DNS", &net.DNSError{Err: "temporary", IsTemporary: true}, 3},
		{"permanent DNS", &net.DNSError{Err: "no such host", IsNotFound: true}, 1},
		{"TLS validation", x509.UnknownAuthorityError{}, 1},
		{"canceled", context.Canceled, 1},
		{"permanent error", errors.New("invalid URL"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					return nil, tc.err
				})}
				resp, err := Do(client, request(t, http.MethodHead))
				if resp != nil || !errors.Is(err, tc.err) || calls != tc.want {
					t.Fatalf("response=%v error=%v calls=%d, want preserved error after %d calls", resp, err, calls, tc.want)
				}
			})
		})
	}
}

func TestDoPreservesPermanentHTTPResponses(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				resp := response(req, status, http.NoBody)
				resp.Header.Set("WWW-Authenticate", `Bearer realm="https://registry.example.test/auth"`)
				return resp, nil
			})}
			resp, err := Do(client, request(t, http.MethodHead))
			if err != nil || resp.StatusCode != status || calls != 1 || resp.Header.Get("WWW-Authenticate") == "" {
				t.Fatalf("response=%v error=%v calls=%d", resp, err, calls)
			}
			resp.Body.Close()
		})
	}
}

func TestDoHonorsCancellation(t *testing.T) {
	for _, duringRequest := range []bool{true, false} {
		t.Run(map[bool]string{true: "request deadline", false: "retry wait"}[duringRequest], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				calls := 0
				client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					if duringRequest {
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					return response(req, http.StatusServiceUnavailable, http.NoBody), nil
				})}
				started := time.Now()
				resp, err := Do(client, request(t, http.MethodHead).WithContext(ctx))
				if resp != nil || !errors.Is(err, context.DeadlineExceeded) || calls != 1 || time.Since(started) != 500*time.Millisecond {
					t.Fatalf("response=%v error=%v calls=%d elapsed=%s", resp, err, calls, time.Since(started))
				}
			})
		})
	}
}

func TestDoDoesNotReplayBodiesOrMutations(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return response(req, http.StatusServiceUnavailable, http.NoBody), nil
			})}
			req := request(t, method)
			if method == http.MethodGet {
				req.Body = io.NopCloser(strings.NewReader("body"))
			}
			resp, err := Do(client, req)
			if err != nil || resp.StatusCode != http.StatusServiceUnavailable || calls != 1 {
				t.Fatalf("response=%v error=%v calls=%d", resp, err, calls)
			}
			resp.Body.Close()
		})
	}
}
