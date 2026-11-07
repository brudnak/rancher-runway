package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercise the real archive handoffs while keeping build tools, signing, Apple
// credentials and network access out of the test.
func TestMacOSReleaseResumesNotarization(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS ditto and PlistBuddy")
	}
	bin, state, dist := t.TempDir(), t.TempDir(), t.TempDir()
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("phased notarization requires jq")
	}
	if err := os.Symlink(jq, filepath.Join(bin, "jq")); err != nil {
		t.Fatal(err)
	}
	write := func(dir, name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	read := func(dir, name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(b))
	}
	shim := func(name, body string) {
		t.Helper()
		write(bin, name, "#!/bin/bash\nset -eu\n"+body)
	}
	for _, name := range []string{"npm", "codesign", "spctl", "rsync"} {
		shim(name, "exit 0")
	}
	shim("go", `while [[ $# -gt 0 ]]; do
  if [[ "$1" == -o ]]; then echo executable > "$2"; exit; fi
  shift
done
exit 1`)
	shim("lipo", `case "$1" in
  -create) [[ "$2" == -output ]]; cp "$4" "$3" ;;
  -archs) echo 'x86_64 arm64' ;;
  *) exit 1 ;;
esac`)
	shim("hdiutil", `[[ "$1" == create ]]
output="${!#}"
while [[ "$1" != -srcfolder ]]; do shift; done
test -f "$2/Rancher Runway.app/ticket"
printf 'disk image\n' > "$output"`)
	const appID = "11111111-1111-4111-8111-111111111111"
	const dmgID = "22222222-2222-4222-8222-222222222222"
	shim("xcrun", `printf '%s\n' "$*" >> "$TRACE"
case "$1 $2" in
  'notarytool submit')
    case "$RANCHER_RUNWAY_RELEASE_PHASE" in
      submit-app) id=$APP_ID; artifact=app.zip ;;
      submit-dmg) id=$DMG_ID; artifact=signed.dmg ;;
      *) echo 'Unexpected submission' >&2; exit 1 ;;
    esac
    [[ " $* " == *" $RANCHER_RUNWAY_NOTARY_STATE_DIR/$artifact "* ]]
    [[ " $* " != *" --wait "* ]]
    printf '{"id":"%s","message":"Successfully uploaded file"}\n' "$id" ;;
  'notarytool wait'|'notarytool info'|'notarytool log')
    if [[ "$RANCHER_RUNWAY_RELEASE_PHASE" == submit-dmg ]]; then id=$APP_ID; else id=$DMG_ID; fi
    [[ " $* " == *" $id "* ]]
    if [[ "$2" == wait && "$APPLE_RESULT" == "In Progress" ]]; then exit 69; fi
    printf '{"id":"%s","status":"%s"}\n' "$id" "$APPLE_RESULT" ;;
  'stapler staple')
    file="${!#}"
    if [[ -d "$file" ]]; then echo ticket > "$file/ticket"; else echo ticket >> "$file"; fi ;;
  'stapler validate')
    file="${!#}"
    if [[ -d "$file" ]]; then test -f "$file/ticket"; else grep -q ticket "$file"; fi ;;
  *) exit 1 ;;
esac`)

	trace := filepath.Join(t.TempDir(), "trace")
	run := func(phase, status string, shouldSucceed bool) {
		t.Helper()
		cmd := exec.Command("bash", "package-macos-release.sh", "v0.1.0")
		cmd.Env = []string{
			"PATH=" + bin + ":/usr/bin:/bin:/usr/sbin:/sbin", "TMPDIR=" + t.TempDir(),
			"RANCHER_RUNWAY_SIGNING_IDENTITY=Developer ID Application: Test (TESTTEAM)",
			"RANCHER_RUNWAY_NOTARY_KEYCHAIN_PROFILE=test-profile",
			"RANCHER_RUNWAY_RELEASE_PHASE=" + phase, "RANCHER_RUNWAY_NOTARY_STATE_DIR=" + state,
			"RANCHER_RUNWAY_OUTPUT_DIR=" + dist, "APPLE_RESULT=" + status,
			"APP_ID=" + appID, "DMG_ID=" + dmgID, "TRACE=" + trace,
		}
		output, err := cmd.CombinedOutput()
		if (err == nil) != shouldSucceed {
			t.Fatalf("%s (%s): %v\n%s", phase, status, err, output)
		}
	}
	run("submit-app", "", true)
	if read(state, "app-id") != appID {
		t.Fatal("app submission ID was not saved")
	}
	run("submit-app", "", false)
	for _, status := range []string{"In Progress", "Invalid"} {
		run("submit-dmg", status, false)
		if read(state, "app-id") != appID || strings.Contains(read(filepath.Dir(trace), "trace"), "stapler staple") {
			t.Fatalf("app should retain its ID and must await acceptance (%s)", status)
		}
	}
	run("submit-dmg", "Accepted", true)
	if read(state, "dmg-id") != dmgID {
		t.Fatal("DMG submission ID was not saved")
	}
	run("submit-dmg", "Accepted", false)
	run("finish", "In Progress", false)
	checksums := "Rancher-Runway-0.1.0-checksums.txt"
	if _, err := os.Stat(filepath.Join(dist, checksums)); !os.IsNotExist(err) {
		t.Fatalf("unexpected checksums before DMG acceptance: %v", err)
	}
	if read(state, "dmg-id") != dmgID {
		t.Fatal("pending DMG lost its submission ID")
	}
	run("finish", "Accepted", true)
	if calls := read(filepath.Dir(trace), "trace"); strings.Count(calls, "notarytool submit ") != 2 {
		t.Fatalf("expected exactly one submission per artifact across retries:\n%s", calls)
	}
	contents, err := exec.Command("unzip", "-Z1", filepath.Join(dist, "Rancher-Runway-0.1.0-macOS-universal.zip")).Output()
	if err != nil || !strings.Contains(string(contents), "Rancher Runway.app/ticket\n") {
		t.Fatalf("published ZIP missing stapled app: %v\n%s", err, contents)
	}
	cmd := exec.Command("shasum", "-a", "256", "-c", checksums)
	cmd.Dir = dist
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("final checksum verification failed: %v\n%s", err, output)
	}
}
