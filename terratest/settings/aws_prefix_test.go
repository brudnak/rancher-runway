package settings

import "testing"

func TestNormalizeAWSPrefixAllowsManualInitials(t *testing.T) {
	got, err := NormalizeAWSPrefix("ATB")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "atb" {
		t.Fatalf("expected lower-case prefix, got %q", got)
	}
}

func TestNormalizeAWSPrefixAllowsAutomationSignoffPrefix(t *testing.T) {
	got, err := NormalizeAWSPrefix("GHA-73161683-FA")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "gha-73161683-fa" {
		t.Fatalf("expected normalized automation prefix, got %q", got)
	}
}

func TestNormalizeAWSPrefixRejectsArbitraryLongPrefix(t *testing.T) {
	if _, err := NormalizeAWSPrefix("github-actions"); err == nil {
		t.Fatal("expected arbitrary long prefix to be rejected")
	}
}

func TestIsAutomationAWSPrefix(t *testing.T) {
	if !IsAutomationAWSPrefix("gha-73161683-fa") {
		t.Fatal("expected sign-off prefix to be treated as automation generated")
	}
	if IsAutomationAWSPrefix("atb") {
		t.Fatal("manual prefix should not be treated as automation generated")
	}
}
