package imagelookup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

var ErrImageLookupCommandOutputLimit = errors.New("command output limit exceeded")

type imageLookupLimitedBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	exceeded bool
}

func (b *imageLookupLimitedBuffer) Write(value []byte) (int, error) {
	remaining := b.limit - int64(b.buffer.Len())
	if remaining <= 0 {
		b.exceeded = true
		return 0, ErrImageLookupCommandOutputLimit
	}
	if int64(len(value)) > remaining {
		written, _ := b.buffer.Write(value[:remaining])
		b.exceeded = true
		return written, ErrImageLookupCommandOutputLimit
	}
	return b.buffer.Write(value)
}

func ExecCommand(ctx context.Context, executable string, arguments, environment []string, outputLimit int64) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Env = environment
	stdout := &imageLookupLimitedBuffer{limit: outputLimit}
	command.Stdout = stdout
	command.Stderr = io.Discard
	err := command.Run()
	if stdout.exceeded {
		return nil, ErrImageLookupCommandOutputLimit
	}
	if err != nil {
		// Preserve bounded stdout for callers that deliberately request structured
		// status information (for example, `gh api --include`). Callers must still
		// treat the accompanying error as authoritative and must not expose the raw
		// command output without validating it first.
		return stdout.buffer.Bytes(), err
	}
	return stdout.buffer.Bytes(), nil
}

func SanitizedGHEnvironment() []string {
	blocked := map[string]struct{}{
		"GH_PROMPT_DISABLED":  {},
		"GIT_TERMINAL_PROMPT": {},
		"GH_PAGER":            {},
		"GH_DEBUG":            {},
	}
	environment := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		key := entry
		if separator := strings.IndexByte(entry, '='); separator >= 0 {
			key = entry[:separator]
		}
		if _, skip := blocked[strings.ToUpper(key)]; skip {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"GH_PROMPT_DISABLED=1",
		"GIT_TERMINAL_PROMPT=0",
		"GH_PAGER=cat",
	)
}

func imageLookupFormatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func SafeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "registry request timed out"
	}
	var registryErr *transport.Error
	if errors.As(err, &registryErr) {
		return fmt.Sprintf("registry returned %d %s", registryErr.StatusCode, http.StatusText(registryErr.StatusCode))
	}
	message := err.Error()
	if len(message) > 240 {
		message = message[:240]
	}
	return message
}

func RegistryNotFound(err error) bool {
	var registryErr *transport.Error
	return errors.As(err, &registryErr) && registryErr.StatusCode == http.StatusNotFound
}

func HTTPStatus(err error) int {
	var inputErr *InputError
	if errors.As(err, &inputErr) {
		return http.StatusBadRequest
	}
	var conflictErr *imageLookupConflictError
	if errors.As(err, &conflictErr) {
		return http.StatusConflict
	}
	var metadataErr *imageLookupSourceMetadataError
	if errors.As(err, &metadataErr) {
		return http.StatusUnprocessableEntity
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return http.StatusGatewayTimeout
	}
	var registryErr *transport.Error
	if errors.As(err, &registryErr) {
		switch registryErr.StatusCode {
		case http.StatusNotFound:
			return http.StatusNotFound
		case http.StatusTooManyRequests:
			return http.StatusTooManyRequests
		case http.StatusUnauthorized, http.StatusForbidden:
			return http.StatusBadGateway
		}
	}
	return http.StatusBadGateway
}
