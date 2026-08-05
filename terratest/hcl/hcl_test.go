package hcl

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestWritePrivateFileAtomicallyUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terraform.tfvars")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("write existing tfvars: %v", err)
	}
	if err := writePrivateFileAtomically(path, []byte("secret\n")); err != nil {
		t.Fatalf("write private tfvars: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat tfvars: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("tfvars mode = %o, want 600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read tfvars: %v", err)
	}
	if string(data) != "secret\n" {
		t.Fatalf("tfvars = %q, want %q", data, "secret\\n")
	}
}

func TestGenAwsVarFileWritesConfiguredAWSRegion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terraform.tfvars")
	GenAwsVarFile(
		path,
		"us-west-2",
		"atb",
		"vpc-123",
		"subnet-a",
		"subnet-b",
		"subnet-c",
		"ami-123",
		"subnet-a",
		"sg-123",
		"qa-key",
		"qa.example.com",
		"",
		"Ada",
		"Lovelace",
		"run-123",
		"ha-rke2",
		0,
		"",
		"m5.large",
		3,
	)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated tfvars: %v", err)
	}
	if !regexp.MustCompile(`(?m)^aws_region\s*=\s*"us-west-2"$`).Match(data) {
		t.Fatalf("generated tfvars did not contain configured AWS region:\n%s", data)
	}
}
