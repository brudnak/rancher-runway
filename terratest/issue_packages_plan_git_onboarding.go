package test

import "fmt"

// Onboarding is a local preference check. It does not inspect credentials,
// check repository reachability, or create a managed checkout.
func (g *issuePackageGitService) onboarding(id string) (map[string]any, error) {
	if id != "" {
		if _, err := g.packages.get(id); err != nil {
			return nil, err
		}
	}
	state, err := g.loadState()
	if err != nil {
		return nil, err
	}
	connection, scope, linkScope := issuePackageGitEffective(state, id)
	linked := connection.Repository != "" || state.DefaultConnection.Repository != ""
	out := map[string]any{
		"required":        !state.OnboardingDismissed && !linked,
		"dismissed":       state.OnboardingDismissed,
		"connection":      nil,
		"connectionScope": scope,
		"linkScope":       linkScope,
	}
	if connection.Repository != "" {
		out["connection"] = connection
	}
	return out, nil
}

func (g *issuePackageGitService) dismissOnboarding(req issuePackageRequest) (map[string]any, error) {
	if !req.DontAskAgain {
		return nil, fmt.Errorf("choose Don't ask again to remember this preference; Skip does not change repository settings")
	}
	if req.ID != "" {
		if _, err := g.packages.get(req.ID); err != nil {
			return nil, err
		}
	}
	state, err := g.loadState()
	if err != nil {
		return nil, err
	}
	state.OnboardingDismissed = true
	if err := g.saveState(state); err != nil {
		return nil, err
	}
	return g.onboarding(req.ID)
}
