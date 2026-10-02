package test

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

// Read only the password needed for this explicit sign-in. Never return the
// config or password to the browser, logs, or operation history.
func (p *localControlPanel) useConfiguredRancherPassword(in *rancherTokenRequest) error {
	if !in.UseBootstrapPassword {
		return nil
	}
	if in.Password != "" || in.Username != "admin" {
		return errors.New("Use the configured bootstrap password with admin, or enter a local username and password manually.")
	}
	data, err := os.ReadFile(p.configPath)
	if err != nil {
		return errors.New("Could not read the Runway config. Enter the Rancher password manually or check your configuration.")
	}
	var config struct {
		Rancher struct {
			Password string `yaml:"bootstrap_password"`
		} `yaml:"rancher"`
	}
	if yaml.Unmarshal(data, &config) != nil {
		return errors.New("Could not parse the Runway config. Check its YAML or enter the password manually.")
	}
	if config.Rancher.Password == "" {
		return errors.New("No rancher.bootstrap_password is set in the Runway config. Enter the Rancher password manually or update the config.")
	}
	in.Password = config.Rancher.Password
	return nil
}
