package test

import (
	"errors"
	"fmt"
	"net/http"
)

// A schema 404 alone does not establish that the driver is inactive. Check its
// actual state before suggesting activation; never change it during discovery.
func deployedDriverSchemaError(provider string, schemaErr error, call func(string, any) error) error {
	label := "Linode"
	if provider == "amazonec2" {
		label = "Amazon EC2"
	}
	var responseErr *deployedRancherHTTPError
	if !errors.As(schemaErr, &responseErr) {
		return fmt.Errorf("could not load %s machine options: %w", label, schemaErr)
	}
	switch responseErr.status {
	case http.StatusUnauthorized:
		return fmt.Errorf("Rancher rejected the token while loading %s options. Sign in again or supply a valid API token", label)
	case http.StatusForbidden:
		return fmt.Errorf("this Rancher user cannot read %s machine options. Check the user's permissions", label)
	case http.StatusNotFound:
		var driver struct {
			Active *bool `json:"active"`
		}
		if err := call("/v3/nodeDrivers/"+provider, &driver); err == nil && driver.Active != nil {
			if !*driver.Active {
				return fmt.Errorf("%s node driver is inactive in this Rancher, so machine options are unavailable. Open Cluster Management → Providers → Node Drivers, activate %s, then load options again", label, label)
			}
			return fmt.Errorf("%s node driver is enabled, but its machine schema is not available yet. Check the driver's status in Rancher, wait for it to become ready, then load options again", label)
		}
		return fmt.Errorf("Rancher did not expose the %s machine schema, and Runway could not verify the driver's active state. Check Node Drivers and this user's permissions, then load options again", label)
	default:
		return fmt.Errorf("could not load %s machine options: %w", label, schemaErr)
	}
}
