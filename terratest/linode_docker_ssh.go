package test

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh"
)

const linodeDockerRancherContainerName = "rancher"

func runLinodeDockerSSHCommand(linodeIP string, rootPassword string, command string) (string, error) {
	linodeIP = strings.TrimSpace(linodeIP)
	if linodeIP == "" {
		return "", fmt.Errorf("missing Linode IP")
	}
	rootPassword = strings.TrimSpace(rootPassword)
	if rootPassword == "" {
		return "", fmt.Errorf("missing root SSH password")
	}
	config := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.Password(rootPassword)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         linodeDockerSSHTimeout(),
	}
	client, err := ssh.Dial("tcp", net.JoinHostPort(linodeIP, "22"), config)
	if err != nil {
		return "", err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	return string(output), err
}

func linodeDockerSSHTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv("LINODE_DOCKER_SSH_TIMEOUT"))
	if value == "" {
		return 12 * time.Second
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 12 * time.Second
	}
	return duration
}

func linodeDockerStatusCommand() string {
	return fmt.Sprintf("docker ps -a --filter name=^/%s$ --format '{{.Names}} {{.Image}} {{.Status}}' || true", linodeDockerRancherContainerName)
}

func linodeDockerInspectCommand() string {
	return fmt.Sprintf("docker inspect --format 'name={{.Name}} image={{.Config.Image}} status={{.State.Status}} running={{.State.Running}} started={{.State.StartedAt}} finished={{.State.FinishedAt}} exit={{.State.ExitCode}} error={{.State.Error}}' %s 2>&1 || true", linodeDockerRancherContainerName)
}

func linodeDockerLogsCommand(tail int) string {
	if tail <= 0 {
		tail = 120
	}
	return fmt.Sprintf("docker logs --tail=%d %s 2>&1 || true", tail, linodeDockerRancherContainerName)
}

func linodeDockerLogSnapshotCommand(tail int) string {
	return strings.Join([]string{
		"printf '### docker ps -a\\n'",
		"docker ps -a --no-trunc || true",
		"printf '\\n### docker inspect rancher\\n'",
		linodeDockerInspectCommand(),
		"printf '\\n### docker logs\\n'",
		linodeDockerLogsCommand(tail),
	}, "\n")
}

func dockerStatusSummary(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "container not listed"
	}
	line := strings.Split(output, "\n")[0]
	line = strings.TrimSpace(line)
	if line == "" {
		return "container not listed"
	}
	if len(line) > 220 {
		line = line[:220] + "..."
	}
	return line
}

func sanitizeDiagnosticOutput(output string) string {
	replacements := []string{
		viper.GetString("rancher.bootstrap_password"),
		linodeRootPassword(),
		os.Getenv("RANCHER_BOOTSTRAP_PASSWORD"),
		os.Getenv("LINODE_TOKEN"),
		os.Getenv("DOCKERHUB_PASSWORD"),
	}
	for _, value := range replacements {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		output = strings.ReplaceAll(output, value, "***")
	}
	return output
}

func lastNonEmptyLines(output string, maxLines int) string {
	output = strings.TrimSpace(output)
	if output == "" || maxLines <= 0 {
		return output
	}
	lines := strings.Split(output, "\n")
	if len(lines) <= maxLines {
		return output
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}
