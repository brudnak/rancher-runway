package prbuild

func prBuildRegistryStatus(result RegistryResult) string {
	if result.Server.Error != "" || result.Agent.Error != "" {
		return "error"
	}
	if result.Server.Found && result.Agent.Found {
		return "complete"
	}
	if result.Server.Found || result.Agent.Found {
		return "partial"
	}
	return "missing"
}

func summarizePRBuildResults(registries []RegistryResult) Summary {
	summary := Summary{RegistryCount: len(registries), ScanComplete: true}
	for _, registry := range registries {
		if registry.PairAvailable {
			summary.CompletePairRegistries++
		}
		if registry.Server.Error != "" {
			summary.ServerErrorRegistries++
			summary.ScanComplete = false
		} else if !registry.Server.Found {
			summary.ServerMissingRegistries++
		} else {
			switch registry.Server.Match.Verdict {
			case "included":
				summary.ServerIncludedRegistries++
			case "not_included":
				summary.ServerNotIncludedRegistries++
			default:
				summary.ServerUnknownRegistries++
			}
		}
		for _, image := range []ImageResult{registry.Server, registry.Agent} {
			if image.Error != "" || image.Match.ComparisonError {
				summary.ScanComplete = false
			}
		}
	}
	switch {
	case summary.ServerIncludedRegistries > 0:
		summary.Verdict = "included"
	case summary.ServerUnknownRegistries > 0 || summary.ServerErrorRegistries > 0:
		summary.Verdict = "unknown"
	case summary.ServerNotIncludedRegistries > 0:
		summary.Verdict = "not_included"
	default:
		summary.Verdict = "unknown"
	}
	return summary
}
