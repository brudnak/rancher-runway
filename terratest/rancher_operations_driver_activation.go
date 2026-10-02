package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type deployedDriverState struct {
	Active *bool `json:"active"`
}

func deployedDriverStatus(ctx context.Context, cluster clusterView, in rancherOperationRequest) (deployedDriverState, error) {
	var state deployedDriverState
	if in.Provider != "linode" && in.Provider != "amazonec2" {
		return state, fmt.Errorf("choose Linode or AWS")
	}
	client, err := deployedRancherClient(in)
	if err != nil {
		return state, err
	}
	defer client.CloseIdleConnections()
	err = deployedRancherJSON(ctx, client, cluster.RancherURL, in.Token, http.MethodGet, "/v3/nodeDrivers/"+in.Provider, nil, &state)
	return state, err
}

// Invoked only by the explicit enable action, never while browsing options.
// Use the existing driver's fixed endpoint; do not accept a driver URL or
// install a new driver supplied by the client.
func enableDeployedDriver(ctx context.Context, cluster clusterView, in rancherOperationRequest) (any, error) {
	state, err := deployedDriverStatus(ctx, cluster, in)
	if err != nil {
		return nil, fmt.Errorf("could not verify the node driver before activation: %w", err)
	}
	if state.Active == nil {
		return nil, fmt.Errorf("Rancher did not report the driver's active state; inspect Node Drivers before retrying")
	}
	client, err := deployedRancherClient(in)
	if err != nil {
		return nil, err
	}
	defer client.CloseIdleConnections()
	if !*state.Active {
		if err = deployedRancherJSON(ctx, client, cluster.RancherURL, in.Token, http.MethodPost, "/v3/nodeDrivers/"+in.Provider+"?action=activate", map[string]any{}, nil); err != nil {
			return nil, fmt.Errorf("could not activate the node driver; check this user's permissions in Rancher: %w", err)
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for {
		_, err = deployedMachineFields(in.Provider, func(path string, out any) error {
			return deployedRancherJSON(waitCtx, client, cluster.RancherURL, in.Token, http.MethodGet, path, nil, out)
		})
		if err == nil {
			return map[string]bool{"ready": true}, nil
		}
		status, isHTTP := err.(*deployedRancherHTTPError)
		if !(isHTTP && status.status == http.StatusNotFound) && !errors.Is(err, errDeployedMachineFieldsPending) {
			return nil, fmt.Errorf("driver activation was requested, but machine options could not be checked. Inspect Node Drivers, then load options again: %w", err)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return nil, fmt.Errorf("driver activation was requested, but machine options are not ready yet. Check Node Drivers in Rancher, then load options again")
		case <-timer.C:
		}
	}
}
