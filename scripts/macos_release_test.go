package scripts

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseSigningConfiguration(t *testing.T) {
	requireBash(t)
	for _, tc := range []struct {
		name string
		env  []string
		want string
	}{
		{"unknown mode", []string{"RANCHER_RUNWAY_SIGNING_MODE=invalid"}, "must be adhoc or developer-id"},
		{"missing identity", []string{"RANCHER_RUNWAY_SIGNING_MODE=developer-id"}, "requires RANCHER_RUNWAY_SIGNING_IDENTITY"},
		{"ad-hoc identity in developer mode", []string{"RANCHER_RUNWAY_SIGNING_MODE=developer-id", "RANCHER_RUNWAY_SIGNING_IDENTITY=-"}, "requires RANCHER_RUNWAY_SIGNING_IDENTITY"},
		{"missing notary credentials", []string{"RANCHER_RUNWAY_SIGNING_MODE=developer-id", "RANCHER_RUNWAY_SIGNING_IDENTITY=Developer ID Application: Test (TESTTEAM)"}, "requires notarization credentials"},
		{"partial notary credentials", []string{"RANCHER_RUNWAY_SIGNING_MODE=developer-id", "RANCHER_RUNWAY_SIGNING_IDENTITY=Developer ID Application: Test (TESTTEAM)", "APPLE_ID=test@example.com", "APPLE_TEAM_ID=TESTTEAM"}, "requires notarization credentials"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "package-macos-release.sh", "v0.1.0")
			cmd.Env = append(releaseTestEnv(t), tc.env...)
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), tc.want) {
				t.Fatalf("expected preflight error %q, got %v\n%s", tc.want, err, output)
			}
		})
	}
}

func TestReleaseSigningModesReachBuild(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("packaging requires macOS")
	}
	requireBash(t)
	for _, name := range []string{"go", "xcrun", "codesign", "ditto", "hdiutil", "lipo", "plutil", "rsync", "shasum"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("macOS packaging prerequisite %s unavailable: %v", name, err)
		}
	}
	for _, tc := range []struct {
		name string
		env  []string
		mode string
	}{
		{"default without Apple secrets", nil, "adhoc"},
		{"explicit adhoc ignores leftover Apple configuration", []string{"RANCHER_RUNWAY_SIGNING_MODE=adhoc", "RANCHER_RUNWAY_SIGNING_IDENTITY=Developer ID Application: Test (TESTTEAM)", "APPLE_ID=test@example.com"}, "adhoc"},
		{"developer ID with keychain", []string{"RANCHER_RUNWAY_SIGNING_MODE=developer-id", "RANCHER_RUNWAY_SIGNING_IDENTITY=Developer ID Application: Test (TESTTEAM)", "RANCHER_RUNWAY_NOTARY_KEYCHAIN_PROFILE=test-profile"}, "developer-id"},
		{"developer ID with Apple credentials", []string{"RANCHER_RUNWAY_SIGNING_MODE=developer-id", "RANCHER_RUNWAY_SIGNING_IDENTITY=Developer ID Application: Test (TESTTEAM)", "APPLE_ID=test@example.com", "APPLE_TEAM_ID=TESTTEAM", "APPLE_APP_SPECIFIC_PASSWORD=test-password"}, "developer-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", "package-macos-release.sh", "v0.1.0")
			cmd.Env = append(releaseTestEnv(t), tc.env...)
			output, err := cmd.CombinedOutput()
			exitErr, ok := err.(*exec.ExitError)
			if !ok || exitErr.ExitCode() != 73 || !strings.Contains(string(output), "reached npm build boundary") || !strings.Contains(string(output), "Signing mode: "+tc.mode) {
				t.Fatalf("did not reach build in %s mode: %v\n%s", tc.mode, err, output)
			}
		})
	}
}

func TestRenderedCaskSigningModes(t *testing.T) {
	requireBash(t)
	for _, mode := range []string{"", "adhoc", "developer-id", "invalid"} {
		t.Run("mode="+mode, func(t *testing.T) {
			tempDir := t.TempDir()
			asset := []byte("test release artifact\n")
			dmgPath := filepath.Join(tempDir, "Rancher-Runway-0.1.0-macOS-universal.dmg")
			if err := os.WriteFile(dmgPath, asset, 0o644); err != nil {
				t.Fatal(err)
			}
			caskPath := filepath.Join(tempDir, "rancher-runway.rb")
			cmd := exec.Command("bash", "render-homebrew-cask.sh", "v0.1.0", dmgPath, caskPath)
			cmd.Env = append(releaseTestEnv(t), "RANCHER_RUNWAY_SIGNING_MODE="+mode, "RANCHER_RUNWAY_RELEASE_REPOSITORY=test-owner/test-releases")
			output, err := cmd.CombinedOutput()
			if mode == "invalid" {
				if err == nil || !strings.Contains(string(output), "must be adhoc or developer-id") {
					t.Fatalf("invalid signing mode accepted: %v\n%s", err, output)
				}
				if _, err := os.Stat(caskPath); !os.IsNotExist(err) {
					t.Fatalf("invalid mode wrote a Cask: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("render: %v\n%s", err, output)
			}
			data, err := os.ReadFile(caskPath)
			if err != nil {
				t.Fatal(err)
			}
			cask := string(data)
			for _, want := range []string{
				fmt.Sprintf(`sha256 "%x"`, sha256.Sum256(asset)),
				"https://github.com/test-owner/test-releases/releases/download/v0.1.0/" + filepath.Base(dmgPath),
				`app "Rancher Runway.app"`,
			} {
				if !strings.Contains(cask, want) {
					t.Errorf("rendered Cask missing %q", want)
				}
			}
			wantNotice := mode != "developer-id"
			if strings.Contains(cask, "not notarized by Apple") != wantNotice || strings.Contains(cask, "Open Anyway") != wantNotice {
				t.Errorf("Cask first-launch notice does not match signing mode %q", mode)
			}
			if strings.Contains(cask, "@ADHOC_") || strings.Contains(cask, "@VERSION@") || strings.Contains(cask, "@SHA256@") {
				t.Error("Cask contains unresolved template placeholders")
			}
			if ruby, err := exec.LookPath("ruby"); err == nil {
				if output, err := exec.Command(ruby, "-c", caskPath).CombinedOutput(); err != nil {
					t.Fatalf("invalid Cask Ruby: %v\n%s", err, output)
				}
			}
		})
	}
}

func releaseTestEnv(t *testing.T) []string {
	t.Helper()
	// Stop at the first build command, including if a rejection test regresses.
	// No test may install dependencies or use real signing credentials.
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "npm"), []byte("#!/bin/bash\necho 'reached npm build boundary'\nexit 73\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "RANCHER_RUNWAY_") && !strings.HasPrefix(entry, "APPLE_") && !strings.HasPrefix(entry, "PATH=") {
			env = append(env, entry)
		}
	}
	return append(env, "RANCHER_RUNWAY_OUTPUT_DIR="+t.TempDir(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
