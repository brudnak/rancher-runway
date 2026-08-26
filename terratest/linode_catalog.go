package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brudnak/ha-rancher-rke2/terratest/settings"
)

const linodeCatalogDefaultAPIBaseURL = "https://api.linode.com/v4"

type linodeCatalogResponse struct {
	Source      string                        `json:"source"`
	CollectedAt time.Time                     `json:"collectedAt"`
	Defaults    settings.LinodeDownstreamPlan `json:"defaults"`
	Regions     []linodeCatalogRegion         `json:"regions"`
	Types       []linodeCatalogType           `json:"types"`
	Images      []linodeCatalogImage          `json:"images"`
}

type linodeCatalogRegion struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Country string `json:"country,omitempty"`
}

type linodeCatalogType struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	MemoryMB   int     `json:"memoryMB,omitempty"`
	VCPUs      int     `json:"vcpus,omitempty"`
	DiskMB     int     `json:"diskMB,omitempty"`
	Class      string  `json:"class,omitempty"`
	HourlyUSD  float64 `json:"hourlyUSD,omitempty"`
	MonthlyUSD float64 `json:"monthlyUSD,omitempty"`
}

type linodeCatalogImage struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Deprecated  bool   `json:"deprecated,omitempty"`
}

type linodeAPIRegion struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Country string `json:"country"`
	Status  string `json:"status"`
}

type linodeAPIType struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Memory int    `json:"memory"`
	VCPUs  int    `json:"vcpus"`
	Disk   int    `json:"disk"`
	Class  string `json:"class"`
	Price  struct {
		Hourly  float64 `json:"hourly"`
		Monthly float64 `json:"monthly"`
	} `json:"price"`
}

type linodeAPIImage struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Status      string `json:"status"`
	IsPublic    bool   `json:"is_public"`
	Deprecated  bool   `json:"deprecated"`
}

type linodeAPIPage[T any] struct {
	Data  []T `json:"data"`
	Page  int `json:"page"`
	Pages int `json:"pages"`
}

func collectLinodeCatalog(ctx context.Context, client *http.Client, baseURL, token string) (linodeCatalogResponse, error) {
	if strings.TrimSpace(token) == "" {
		return linodeCatalogResponse{}, fmt.Errorf("LINODE_TOKEN or LINODE_ACCESS_TOKEN is required to load downstream options")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = linodeCatalogDefaultAPIBaseURL
	}

	var regions []linodeAPIRegion
	var types []linodeAPIType
	var images []linodeAPIImage
	var fetchErr error
	var mu sync.Mutex
	var wg sync.WaitGroup
	fetch := func(endpoint string, destination any) {
		defer wg.Done()
		if err := fetchLinodeCatalogCollection(ctx, client, baseURL+endpoint, token, destination); err != nil {
			mu.Lock()
			if fetchErr == nil {
				fetchErr = err
			}
			mu.Unlock()
		}
	}
	wg.Add(3)
	go fetch("/regions", &regions)
	go fetch("/linode/types", &types)
	go fetch("/images", &images)
	wg.Wait()
	if fetchErr != nil {
		return linodeCatalogResponse{}, fetchErr
	}

	result := linodeCatalogResponse{
		Source:      "Linode API v4",
		CollectedAt: time.Now().UTC(),
		Defaults:    settings.DefaultLinodeDownstreamPlan(),
	}
	for _, region := range regions {
		if !strings.EqualFold(strings.TrimSpace(region.Status), "ok") {
			continue
		}
		result.Regions = append(result.Regions, linodeCatalogRegion{
			ID:      strings.TrimSpace(region.ID),
			Label:   firstNonEmptyCatalogValue(strings.TrimSpace(region.Label), strings.TrimSpace(region.ID)),
			Country: strings.TrimSpace(region.Country),
		})
	}
	for _, instanceType := range types {
		if strings.TrimSpace(instanceType.ID) == "" {
			continue
		}
		result.Types = append(result.Types, linodeCatalogType{
			ID:         strings.TrimSpace(instanceType.ID),
			Label:      formatLinodeCatalogTypeLabel(instanceType),
			MemoryMB:   instanceType.Memory,
			VCPUs:      instanceType.VCPUs,
			DiskMB:     instanceType.Disk,
			Class:      strings.TrimSpace(instanceType.Class),
			HourlyUSD:  instanceType.Price.Hourly,
			MonthlyUSD: instanceType.Price.Monthly,
		})
	}
	for _, image := range images {
		if !strings.EqualFold(strings.TrimSpace(image.Status), "available") || strings.TrimSpace(image.ID) == "" {
			continue
		}
		result.Images = append(result.Images, linodeCatalogImage{
			ID:          strings.TrimSpace(image.ID),
			Label:       firstNonEmptyCatalogValue(strings.TrimSpace(image.Label), strings.TrimSpace(image.ID)),
			Description: strings.TrimSpace(image.Description),
			Deprecated:  image.Deprecated,
		})
	}

	sort.SliceStable(result.Regions, func(i, j int) bool { return result.Regions[i].ID < result.Regions[j].ID })
	sort.SliceStable(result.Images, func(i, j int) bool {
		return strings.ToLower(result.Images[i].Label) < strings.ToLower(result.Images[j].Label)
	})
	if len(result.Regions) == 0 || len(result.Types) == 0 || len(result.Images) == 0 {
		return linodeCatalogResponse{}, fmt.Errorf("Linode API returned an incomplete catalog (%d regions, %d types, %d images)", len(result.Regions), len(result.Types), len(result.Images))
	}
	return result, nil
}

func firstNonEmptyCatalogValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func formatLinodeCatalogTypeLabel(instanceType linodeAPIType) string {
	label := firstNonEmptyCatalogValue(strings.TrimSpace(instanceType.Label), strings.TrimSpace(instanceType.ID))
	if instanceType.VCPUs < 1 || instanceType.Disk < 1 {
		return label
	}
	vCPU := "vCPUs"
	if instanceType.VCPUs == 1 {
		vCPU = "vCPU"
	}
	diskGB := float64(instanceType.Disk) / 1024
	disk := fmt.Sprintf("%.1f", diskGB)
	if instanceType.Disk%1024 == 0 {
		disk = fmt.Sprintf("%d", instanceType.Disk/1024)
	}
	return fmt.Sprintf("%s, %d %s, %s GB Disk", label, instanceType.VCPUs, vCPU, disk)
}

func fetchLinodeCatalogCollection(ctx context.Context, client *http.Client, endpoint, token string, destination any) error {
	switch out := destination.(type) {
	case *[]linodeAPIRegion:
		return fetchAllLinodePages(ctx, client, endpoint, token, out)
	case *[]linodeAPIType:
		return fetchAllLinodePages(ctx, client, endpoint, token, out)
	case *[]linodeAPIImage:
		return fetchAllLinodePages(ctx, client, endpoint, token, out)
	default:
		return fmt.Errorf("unsupported Linode catalog destination %T", destination)
	}
}

func fetchAllLinodePages[T any](ctx context.Context, client *http.Client, endpoint, token string, destination *[]T) error {
	for page := 1; page <= 20; page++ {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return fmt.Errorf("invalid Linode catalog endpoint: %w", err)
		}
		query := parsed.Query()
		query.Set("page", fmt.Sprintf("%d", page))
		query.Set("page_size", "500")
		parsed.RawQuery = query.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("Linode catalog request failed: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("failed to read Linode catalog response: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			message := strings.TrimSpace(string(body))
			if secret := strings.TrimSpace(token); secret != "" {
				message = strings.ReplaceAll(message, secret, "[redacted]")
			}
			if len(message) > 500 {
				message = message[:500] + "…"
			}
			return fmt.Errorf("Linode catalog returned HTTP %d: %s", resp.StatusCode, message)
		}
		var payload linodeAPIPage[T]
		if err := json.Unmarshal(body, &payload); err != nil {
			return fmt.Errorf("failed to parse Linode catalog response: %w", err)
		}
		*destination = append(*destination, payload.Data...)
		pages := payload.Pages
		if pages < 1 {
			pages = 1
		}
		if page >= pages {
			return nil
		}
	}
	return fmt.Errorf("Linode catalog exceeded the 20-page safety limit")
}
