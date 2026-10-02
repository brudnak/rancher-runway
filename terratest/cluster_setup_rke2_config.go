package test

import (
	"encoding/json"
	"fmt"
	"strings"
)

func rke2TLSSANs(haOutputs TerraformOutputs) []string {
	values := append([]string{haOutputs.RancherURL}, haOutputs.ServerIPs...)
	values = append(values, haOutputs.ServerPrivateIPs...)
	return nonEmptyStrings(values...)
}

func rke2ConfigListLines(values []string) string {
	lines := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			lines = append(lines, fmt.Sprintf("  - %s", trimmed))
		}
	}
	return strings.Join(lines, "\n")
}

func rke2FirstServerConfigContent(haOutputs TerraformOutputs, ingressController string) string {
	return fmt.Sprintf("tls-san:\n%s\ningress-controller: %s", rke2ConfigListLines(rke2TLSSANs(haOutputs)), ingressController)
}

func rke2AdditionalServerConfigContent(firstServerIP, token string, haOutputs TerraformOutputs, ingressController string) string {
	return fmt.Sprintf(`server: https://%s:9345
token: %s
tls-san:
%s
ingress-controller: %s`,
		firstServerIP,
		token,
		rke2ConfigListLines(rke2TLSSANs(haOutputs)),
		ingressController)
}

func rke2RegistriesConfigContent(username, password string) string {
	quotedUsername := yamlJSONString(username)
	quotedPassword := yamlJSONString(password)
	return fmt.Sprintf(`configs:
  "docker.io":
    auth:
      username: %s
      password: %s
  "registry-1.docker.io":
    auth:
      username: %s
      password: %s
  "index.docker.io":
    auth:
      username: %s
      password: %s`,
		quotedUsername, quotedPassword,
		quotedUsername, quotedPassword,
		quotedUsername, quotedPassword)
}

func yamlJSONString(value string) string {
	quoted, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%q", value)
	}
	return string(quoted)
}
