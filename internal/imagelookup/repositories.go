package imagelookup

// RancherServerRepositories returns production server repositories in preference
// order. Older builds remain available under rancher/rancher.
func RancherServerRepositories() []string {
	return []string{"rancher/rancher-server", "rancher/rancher"}
}

// IsRancherServerRepository accepts only canonical server repository names.
func IsRancherServerRepository(repository string) bool {
	return repository == "rancher/rancher-server" || repository == "rancher/rancher"
}
