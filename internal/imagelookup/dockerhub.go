package imagelookup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type imageLookupDockerHubTag struct {
	Name          string    `json:"name"`
	FullSize      int64     `json:"full_size"`
	TagLastPushed time.Time `json:"tag_last_pushed"`
	Images        []struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"images"`
}

func (s *Service) dockerHubTag(ctx context.Context, repository, tag string) (imageLookupDockerHubTag, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return imageLookupDockerHubTag{}, errors.New("Docker Hub metadata requires namespace/repository")
	}
	endpoint := "https://hub.docker.com/v2/namespaces/" + url.PathEscape(parts[0]) + "/repositories/" + url.PathEscape(parts[1]) + "/tags/" + url.PathEscape(tag)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return imageLookupDockerHubTag{}, err
	}
	response, err := (&http.Client{Transport: s.transport}).Do(request)
	if err != nil {
		return imageLookupDockerHubTag{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return imageLookupDockerHubTag{}, fmt.Errorf("Docker Hub metadata returned %s", response.Status)
	}
	var result imageLookupDockerHubTag
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(&result); err != nil {
		return imageLookupDockerHubTag{}, err
	}
	return result, nil
}

func (s *Service) enrichDockerHubTags(ctx context.Context, repository, query string, tags []Tag, requireComplete bool) (bool, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return false, errors.New("Docker Hub metadata requires namespace/repository")
	}
	if len(tags) == 0 {
		return true, nil
	}
	indexes := make(map[string][]int, len(tags))
	remaining := make(map[string]struct{}, len(tags))
	for index := range tags {
		indexes[tags[index].Name] = append(indexes[tags[index].Name], index)
		remaining[tags[index].Name] = struct{}{}
	}
	endpointURL, err := url.Parse("https://hub.docker.com/v2/namespaces/" + url.PathEscape(parts[0]) + "/repositories/" + url.PathEscape(parts[1]) + "/tags")
	if err != nil {
		return false, err
	}
	maxPages := 1
	if requireComplete {
		maxPages = (s.maxTagScan + 99) / 100
		if maxPages < 1 {
			maxPages = 1
		}
	}
	client := &http.Client{
		Transport: s.transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for pageNumber := 1; pageNumber <= maxPages; pageNumber++ {
		pageURL := *endpointURL
		values := pageURL.Query()
		values.Set("page_size", "100")
		values.Set("page", strconv.Itoa(pageNumber))
		if query != "" && !imageLookupQuickQuery(query) {
			values.Set("name", query)
		}
		pageURL.RawQuery = values.Encode()
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, pageURL.String(), nil)
		if requestErr != nil {
			return false, requestErr
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			return false, requestErr
		}
		var page struct {
			Next    string                    `json:"next"`
			Results []imageLookupDockerHubTag `json:"results"`
		}
		decodeErr := func() error {
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return fmt.Errorf("Docker Hub metadata returned %s", response.Status)
			}
			return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&page)
		}()
		if decodeErr != nil {
			return false, decodeErr
		}
		for _, metadata := range page.Results {
			matchedIndexes, ok := indexes[metadata.Name]
			if !ok {
				continue
			}
			for _, index := range matchedIndexes {
				tags[index].UploadedAt = imageLookupFormatTime(metadata.TagLastPushed)
				tags[index].Size = metadata.FullSize
				if len(metadata.Images) == 1 {
					tags[index].Digest = metadata.Images[0].Digest
				}
			}
			if !metadata.TagLastPushed.IsZero() {
				delete(remaining, metadata.Name)
			}
		}
		if len(remaining) == 0 {
			return true, nil
		}
		if page.Next == "" {
			return false, nil
		}
	}
	return false, errors.New("Docker Hub tag metadata exceeded the bounded pagination limit")
}
