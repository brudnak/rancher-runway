package test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// testLabDarwinToolchainEnv configures native builds without importing the
// launch shell's credentials, compiler overrides, or stale SDKROOT. Runway's
// tool PATH can find the raw CLT clang before Apple's SDK-aware /usr/bin shim.
func testLabDarwinToolchainEnv(ctx context.Context, env []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	env = append([]string{}, env...)
	// Honor an explicitly selected Xcode installation; this is the only host
	// toolchain setting admitted into the isolated runner environment.
	if developerDir := os.Getenv("DEVELOPER_DIR"); developerDir != "" {
		env = append(env, "DEVELOPER_DIR="+developerDir)
	}
	sdkCommand := exec.CommandContext(ctx, "/usr/bin/xcrun", "--sdk", "macosx", "--show-sdk-path")
	sdkCommand.Env = env
	sdkCommand.WaitDelay = 2 * time.Second
	sdkOutput, err := sdkCommand.Output()
	if err != nil {
		detail := err.Error()
		if exitError, ok := err.(*exec.ExitError); ok {
			detail += ": " + strings.TrimSpace(string(exitError.Stderr))
		}
		return nil, fmt.Errorf("could not locate the macOS SDK: %s", detail)
	}
	sdk := strings.TrimSpace(string(sdkOutput))
	info, err := os.Stat(sdk)
	if !filepath.IsAbs(sdk) || err != nil || !info.IsDir() {
		return nil, fmt.Errorf("the selected macOS SDK is not an accessible directory")
	}
	env = append(env, "SDKROOT="+sdk, "CC=/usr/bin/clang", "CXX=/usr/bin/clang++", "CGO_ENABLED=1")
	// Check the exact headers required by runtime/cgo before Go spends time
	// downloading modules. Syntax-only compilation leaves no build artifacts.
	compiler := exec.CommandContext(ctx, "/usr/bin/clang", "-fsyntax-only", "-x", "c", "-")
	compiler.Env = env
	compiler.Stdin = strings.NewReader("#include <stdlib.h>\n#include <errno.h>\n#include <pthread.h>\nint main(void) { return 0; }\n")
	compiler.WaitDelay = 2 * time.Second
	if output, err := compiler.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("the macOS C compiler could not read its SDK headers: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return env, nil
}
