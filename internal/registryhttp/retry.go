// Package registryhttp provides bounded retries for registry metadata lookups.
package registryhttp

import (
	"errors"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"
)

// Do retries transient failures of bodyless GET and HEAD requests up to three
// times in total, waiting one then two seconds. The client's timeout applies to
// each attempt; the request context bounds the entire operation. The caller owns
// the final response body, including when the final HTTP status is unsuccessful.
func Do(client *http.Client, req *http.Request) (*http.Response, error) {
	if (req.Method != http.MethodGet && req.Method != http.MethodHead) || (req.Body != nil && req.Body != http.NoBody) {
		return client.Do(req)
	}

	for attempt := 1; ; attempt++ {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if attempt == 3 || !retryable(resp, err) || req.Context().Err() != nil {
			return resp, err
		}
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
		}

		timer := time.NewTimer(time.Duration(attempt) * time.Second)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

func retryable(resp *http.Response, err error) bool {
	if err != nil {
		var networkError net.Error
		return (errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())) ||
			errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
			errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
	}
	switch resp.StatusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
