package prbuild

import (
	"errors"
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"net/http"
)

func HTTPStatus(err error) int {
	var githubErr *GitHubHTTPError
	if errors.As(err, &githubErr) {
		switch githubErr.Status {
		case http.StatusNotFound:
			return http.StatusNotFound
		case http.StatusTooManyRequests:
			return http.StatusTooManyRequests
		default:
			return http.StatusBadGateway
		}
	}
	return imagelookup.HTTPStatus(err)
}
