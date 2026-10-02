// Package linodeinventory provides token-scoped discovery and single-resource cleanup.
package linodeinventory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var paths = map[string]string{"instance": "/linode/instances", "volume": "/volumes", "nodebalancer": "/nodebalancers", "firewall": "/networking/firewalls"}
var kinds = []string{"instance", "volume", "nodebalancer", "firewall"}

type Ref struct {
	Type string `json:"type"`
	ID   int    `json:"id"`
}
type Resource struct {
	Ref
	Label      string   `json:"label"`
	Region     string   `json:"region,omitempty"`
	Status     string   `json:"status,omitempty"`
	Tags       []string `json:"tags"`
	IPv4       []string `json:"ipv4,omitempty"`
	Size       int      `json:"size,omitempty"`
	AttachedTo *int     `json:"attachedTo,omitempty"`
	Created    string   `json:"created,omitempty"`
}
type Inventory struct {
	Items       []Resource `json:"items"`
	Warnings    []string   `json:"warnings"`
	CollectedAt time.Time  `json:"collectedAt"`
}
type Client struct {
	HTTP    *http.Client
	BaseURL string
}

func (c Client) request(ctx context.Context, token, method, path string, out any) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("configure a Linode token in Runway or enter one for this session")
	}
	base := c.BaseURL
	if base == "" {
		base = "https://api.linode.com/v4"
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return fmt.Errorf("invalid Linode request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	client := http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.HTTP != nil {
		client = *c.HTTP
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("Linode request failed; check connectivity and token access")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Linode returned HTTP %d for %s; verify token permissions and resource state", response.StatusCode, method)
	}
	if out == nil {
		return nil
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("Linode returned an unreadable response")
	}
	return nil
}

type apiResource struct {
	ID       int             `json:"id"`
	Label    string          `json:"label"`
	Region   string          `json:"region"`
	Status   string          `json:"status"`
	Tags     []string        `json:"tags"`
	IPv4     json.RawMessage `json:"ipv4"`
	Size     int             `json:"size"`
	LinodeID *int            `json:"linode_id"`
	Created  string          `json:"created"`
}

func resource(kind string, a apiResource) Resource {
	var ips []string
	if json.Unmarshal(a.IPv4, &ips) != nil {
		var single string
		if json.Unmarshal(a.IPv4, &single) == nil && single != "" {
			ips = []string{single}
		}
	}
	return Resource{Ref: Ref{kind, a.ID}, Label: a.Label, Region: a.Region, Status: a.Status, Tags: a.Tags, IPv4: ips, Size: a.Size, AttachedTo: a.LinodeID, Created: a.Created}
}
func resourcePath(ref Ref) (string, error) {
	path, ok := paths[ref.Type]
	if !ok || ref.ID <= 0 {
		return "", fmt.Errorf("choose exactly one supported Linode resource with a positive ID")
	}
	return path + "/" + strconv.Itoa(ref.ID), nil
}
func (c Client) Get(ctx context.Context, token string, ref Ref) (Resource, error) {
	path, err := resourcePath(ref)
	if err != nil {
		return Resource{}, err
	}
	var a apiResource
	err = c.request(ctx, token, http.MethodGet, path, &a)
	if err != nil {
		return Resource{}, err
	}
	if a.ID != ref.ID || a.Label == "" {
		return Resource{}, fmt.Errorf("Linode resource identity could not be verified")
	}
	return resource(ref.Type, a), nil
}
func (c Client) List(ctx context.Context, token string) (Inventory, error) {
	result := Inventory{Items: []Resource{}, Warnings: []string{}, CollectedAt: time.Now().UTC()}
	if strings.TrimSpace(token) == "" {
		return result, fmt.Errorf("configure a Linode token in Runway or enter one for this session")
	}
	for _, kind := range kinds {
		for page := 1; page <= 1000; page++ {
			var data struct {
				Data  []apiResource `json:"data"`
				Pages int           `json:"pages"`
			}
			err := c.request(ctx, token, http.MethodGet, paths[kind]+"?page_size=100&page="+strconv.Itoa(page), &data)
			if err != nil {
				result.Warnings = append(result.Warnings, kind+": "+err.Error())
				break
			}
			for _, a := range data.Data {
				result.Items = append(result.Items, resource(kind, a))
			}
			if page >= data.Pages {
				break
			}
			if page == 1000 {
				result.Warnings = append(result.Warnings, kind+": inventory truncated after 1000 pages")
			}
		}
	}
	return result, nil
}

func (c Client) inspect(ctx context.Context, token string, ref Ref) (Resource, []string, error) {
	item, err := c.Get(ctx, token, ref)
	if err != nil {
		return item, nil, err
	}
	effects := []string{"This deletes only the selected resource. No other listed resource will be selected automatically."}
	switch ref.Type {
	case "instance":
		effects = append(effects, "Permanently deletes this instance, its local disks and backups. Attached Block Storage volumes are not selected for deletion. Rancher may recreate a node that is still part of a machine pool; remove that cluster or pool through Rancher first.")
	case "volume":
		if item.AttachedTo != nil && *item.AttachedTo != 0 {
			return item, nil, fmt.Errorf("volume is attached to instance %d; detach it deliberately in Linode before cleanup", *item.AttachedTo)
		}
		effects = append(effects, "Permanently deletes the volume and its data; no snapshot is created.")
	case "nodebalancer":
		effects = append(effects, "Deletes the NodeBalancer and its configurations and backend registrations. Backend servers are retained. Traffic through this load balancer will stop.")
	case "firewall":
		path, _ := resourcePath(ref)
		var devices struct {
			Data    []json.RawMessage `json:"data"`
			Results int               `json:"results"`
		}
		if err = c.request(ctx, token, http.MethodGet, path+"/devices?page_size=100", &devices); err != nil {
			return item, nil, fmt.Errorf("cannot verify firewall attachments: %w", err)
		}
		if len(devices.Data) > 0 || devices.Results > 0 {
			return item, nil, fmt.Errorf("firewall is attached to resources; detach it deliberately in Linode before cleanup")
		}
		effects = append(effects, "Permanently deletes this unattached firewall and its rules.")
	}
	return item, effects, nil
}
