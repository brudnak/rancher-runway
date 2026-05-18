package settings

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

var awsPrefixPattern = regexp.MustCompile(`^[a-z]{2,3}$`)
var automationAWSPrefixPattern = regexp.MustCompile(`^(gha-([a-z]{2,3}-)?[a-z0-9]{1,8}-[a-z]{2}|local-([a-z]{2,3}-)?[a-z]{2})$`)

func NormalizeAWSPrefix(value string) (string, error) {
	prefix := strings.ToLower(strings.TrimSpace(value))
	if !awsPrefixPattern.MatchString(prefix) && !automationAWSPrefixPattern.MatchString(prefix) {
		return "", fmt.Errorf("tf_vars.aws_prefix must be 2 or 3 letters, usually your initials, or an automation-generated sign-off prefix; got %q", strings.TrimSpace(value))
	}
	return prefix, nil
}

func IsAutomationAWSPrefix(value string) bool {
	return automationAWSPrefixPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}

func ValidateAWSPrefixConfig() error {
	prefix, err := NormalizeAWSPrefix(viper.GetString("tf_vars.aws_prefix"))
	if err != nil {
		return err
	}
	viper.Set("tf_vars.aws_prefix", prefix)
	return nil
}
