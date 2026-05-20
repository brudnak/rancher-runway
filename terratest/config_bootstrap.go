package test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const starterToolConfigYAML = `rancher:
  mode: auto
  version: ""
  distro: auto
  bootstrap_password: ""
  auto_approve: false

deployment:
  type: ha-rke2

rke2:
  preload_images: true

total_has: 1

user:
  first_name: ""
  last_name: ""

tf_vars:
  aws_region: ""
  aws_prefix: ""
  aws_vpc: ""
  aws_subnet_a: ""
  aws_subnet_b: ""
  aws_subnet_c: ""
  aws_ami: ""
  aws_subnet_id: ""
  aws_security_group_id: ""
  aws_pem_key_name: ""
  aws_route53_fqdn: ""
  custom_hostname_prefix: ""
`

func ensureStarterToolConfigForPanel(repoRoot string) (string, bool, error) {
	configPath := filepath.Join(repoRoot, "tool-config.yml")
	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return configPath, false, nil
		}
		return configPath, false, fmt.Errorf("failed to create starter tool-config.yml: %w", err)
	}
	defer file.Close()

	if _, err := file.WriteString(starterToolConfigYAML); err != nil {
		_ = os.Remove(configPath)
		return configPath, false, fmt.Errorf("failed to write starter tool-config.yml: %w", err)
	}

	return configPath, true, nil
}
