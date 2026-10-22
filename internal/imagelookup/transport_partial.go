package imagelookup

import (
	"net/http"
	"strconv"
	"strings"
)

// Some registry blob stores (including SUSE staging) return 206 for a full GET,
// even when the response contains the entire object. go-containerregistry only
// accepts 200. Normalize only a single, explicitly complete range; its existing
// blob size and digest verification still checks the body against the manifest.
// This wraps remote requests, including redirects whose blob-storage URLs need
// not retain the registry's /v2/.../blobs/ path.
type imageLookupFullResponseTransport struct {
	inner http.RoundTripper
}

func (t imageLookupFullResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.inner.RoundTrip(request)
	if err != nil || response == nil || request.Method != http.MethodGet || len(request.Header.Values("Range")) != 0 || response.StatusCode != http.StatusPartialContent {
		return response, err
	}
	ranges := response.Header.Values("Content-Range")
	if len(ranges) != 1 || !imageLookupCompleteContentRange(ranges[0], response.ContentLength) || !imageLookupBlobRequest(request) {
		return response, nil
	}

	complete := *response
	complete.StatusCode = http.StatusOK
	complete.Status = "200 OK"
	complete.Header = response.Header.Clone()
	complete.Header.Del("Content-Range")
	return &complete, nil
}

func imageLookupBlobRequest(request *http.Request) bool {
	// The HTTP client retains the preceding response on redirect requests. Follow
	// that chain so only digest-addressed registry blobs receive this workaround,
	// even when the final storage URL has a different host and path.
	for redirects := 0; request != nil && redirects <= 10; redirects++ {
		if request.Method != http.MethodGet || len(request.Header.Values("Range")) != 0 {
			return false
		}
		if request.URL != nil && strings.HasPrefix(request.URL.Path, "/v2/") {
			path := request.URL.Path
			if separator := strings.LastIndex(path, "/blobs/"); separator > len("/v2/") && imageLookupDigestPattern.MatchString(path[separator+len("/blobs/"):]) {
				return true
			}
		}
		if request.Response == nil {
			break
		}
		request = request.Response.Request
	}
	return false
}

func imageLookupCompleteContentRange(value string, contentLength int64) bool {
	value, ok := strings.CutPrefix(value, "bytes 0-")
	if !ok || contentLength <= 0 {
		return false
	}
	endText, totalText, ok := strings.Cut(value, "/")
	if !ok {
		return false
	}
	// Only unsigned decimal byte positions are valid (ParseInt also allows signs).
	for _, text := range []string{endText, totalText} {
		if text == "" || strings.IndexFunc(text, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return false
		}
	}
	end, endErr := strconv.ParseInt(endText, 10, 64)
	total, totalErr := strconv.ParseInt(totalText, 10, 64)
	return endErr == nil && totalErr == nil && total == contentLength && end == total-1
}
