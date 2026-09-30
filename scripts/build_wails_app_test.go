package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWailsBuildPreparesAssetsWithoutLegacyEmbedAnalysis(t *testing.T) {
	requireBash(t)
	repo := filepath.Join(t.TempDir(), "Runway with spaces")
	write := func(name, content string, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"build-wails-app.sh", "app-bundle-utils.sh", "prepare-runtime-data.sh"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		write("scripts/"+name, string(data), 0o755)
	}
	for _, directory := range []string{"desktop/wails", "node_modules/vue"} {
		if err := os.MkdirAll(filepath.Join(repo, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"tailwindcss", "vite"} {
		write("node_modules/.bin/"+name, "#!/usr/bin/env bash\nexit 1\n", 0o755)
	}
	write("tools/uname", "#!/usr/bin/env bash\nprintf 'Darwin\\n'\n", 0o755)
	write("tools/go", "#!/usr/bin/env bash\necho 'Unexpected Go invocation' >&2\nexit 1\n", 0o755)
	write("tools/npm", `#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == 'run build:panel-ui' ]]
mkdir -p terratest/ui/static
printf 'generated\n' > terratest/ui/static/control_panel.css
printf 'generated\n' > terratest/ui/static/control_panel_header_vue.js
`, 0o755)
	write("tools/wails", `#!/usr/bin/env bash
set -euo pipefail
[[ -s "${RANCHER_RUNWAY_REPO}/terratest/ui/static/control_panel.css" ]]
[[ -s "${RANCHER_RUNWAY_REPO}/terratest/ui/static/control_panel_header_vue.js" ]]
[[ -s "${RANCHER_RUNWAY_REPO}/terratest/automation-output/go.mod" ]]
printf '%s\n' "$@" > "${RANCHER_RUNWAY_REPO}/wails-arguments.txt"
`, 0o755)

	cmd := exec.Command("bash", filepath.Join(repo, "scripts", "build-wails-app.sh"))
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Join(repo, "tools")+string(os.PathListSeparator)+os.Getenv("PATH"),
		"WAILS_BIN="+filepath.Join(repo, "tools", "wails"),
		"RANCHER_RUNWAY_BUILD_COMMIT=local",
		"RANCHER_RUNWAY_BUILD_DATE=2026-09-30T00:00:00Z",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build script failed: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(repo, "wails-arguments.txt"))
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(args) == 0 || args[0] != "build" {
		t.Fatalf("expected Wails build, got %q", args)
	}
	skipEmbedAnalysis := false
	for _, arg := range args[1:] {
		switch arg {
		case "-skipembedcreate", "-skipembedcreate=true":
			skipEmbedAnalysis = true
		case "-skipbindings", "-skipbindings=true", "-s", "-s=true", "-skipfrontend", "-skipfrontend=true", "-dryrun", "-dryrun=true":
			t.Fatalf("build must retain bindings, frontend and compilation; found %q", arg)
		}
	}
	if !skipEmbedAnalysis {
		t.Fatal("build invokes Wails' legacy embed type loader, which cannot read newer Go export data")
	}
}
