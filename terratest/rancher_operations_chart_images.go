package test

import (
	"fmt"
	"strings"
)

// Resolve chart defaults using the same precedence as Rancher's chart helpers.
// Explicit head/custom images do not require a nonempty default repository.
func rancherUpgradeImageSettings(chart helmLabChart, appVersion, override string) (repository, tag string, imageFields bool, err error) {
	image, _ := chart.Values["image"].(map[string]any)
	_, hasRepository := image["repository"]
	_, hasTag := image["tag"]
	imageFields = hasRepository && hasTag
	_, hasLegacy := chart.Values["rancherImage"]
	if !imageFields && !hasLegacy {
		return "", "", false, fmt.Errorf("selected chart has no supported Rancher image settings (image.repository/image.tag or rancherImage); choose another chart")
	}
	if override != "" {
		custom, ok, parseErr := parseCustomRancherImageRequest(override)
		if parseErr != nil {
			return "", "", imageFields, parseErr
		}
		if !ok {
			return "", "", imageFields, fmt.Errorf("enter a full Rancher image repository and exact tag")
		}
		return custom.serverRepository, custom.tag, imageFields, nil
	}
	value := func(values map[string]any, key string) string {
		text, _ := values[key].(string)
		return strings.TrimSpace(text)
	}
	repository = value(chart.Values, "rancherImage")
	tag = value(chart.Values, "rancherImageTag")
	if imageFields {
		if repository == "" {
			repository = value(image, "repository")
			if repository != "" {
				registry := value(image, "registry")
				if registry == "" {
					registry = value(chart.Values, "systemDefaultRegistry")
				}
				if registry == "" {
					registry = "docker.io"
				}
				repository = strings.TrimRight(registry, "/") + "/" + repository
			}
		} else if _, _, qualified := splitRegistryRepository(repository); !qualified {
			repository = "docker.io/" + repository
		}
		if tag == "" {
			tag = value(image, "tag")
		}
	}
	if tag == "" {
		tag = appVersion
	}
	if repository == "" || tag == "" {
		return "", "", imageFields, fmt.Errorf("selected chart has no Rancher image repository or version; choose an exact image or another chart")
	}
	return repository, tag, imageFields, nil
}
