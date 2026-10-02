package imagelookup

func PreferredRegistryLabel(registry string) string {
	if registry == "registry.rancher.com" {
		return "Rancher Prime"
	}
	return RegistryLabel(registry)
}
