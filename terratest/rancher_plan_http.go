package test

import (
	"fmt"
	"golang.org/x/net/html"
	"io"
	"net/http"
	"strings"
)

func fetchURLBody(url string) (string, error) {
	body, _, err := fetchURLBodyWithResolvedURL(url)
	return body, err
}

func fetchURLBodyWithResolvedURL(url string) (string, string, error) {
	resp, err := rancherLookupHTTPClient.Get(url)
	if err != nil {
		return "", url, fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	resolvedURL := url
	if resp.Request != nil && resp.Request.URL != nil {
		resolvedURL = resp.Request.URL.String()
	}
	if resp.StatusCode != http.StatusOK {
		return "", resolvedURL, httpStatusError{URL: url, StatusCode: resp.StatusCode}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resolvedURL, fmt.Errorf("failed to read %s: %w", url, err)
	}
	return string(body), resolvedURL, nil
}

func extractTextFromHTML(document string) (string, error) {
	root, err := html.Parse(strings.NewReader(document))
	if err != nil {
		return "", err
	}

	var textParts []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			text := strings.TrimSpace(node.Data)
			if text != "" {
				textParts = append(textParts, text)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)

	return strings.Join(textParts, " "), nil
}
