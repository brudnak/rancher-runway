package test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/spf13/viper"
)

type fakeSSMInstanceInformationDescriber func(context.Context, *ssm.DescribeInstanceInformationInput) (*ssm.DescribeInstanceInformationOutput, error)

func (f fakeSSMInstanceInformationDescriber) DescribeInstanceInformation(ctx context.Context, input *ssm.DescribeInstanceInformationInput, _ ...func(*ssm.Options)) (*ssm.DescribeInstanceInformationOutput, error) {
	return f(ctx, input)
}

func TestWaitForSSMAgentRejectsCallerErrorsImmediately(t *testing.T) {
	for _, code := range []string{"AccessDeniedException", "ExpiredToken", "InvalidClientTokenId", "UnauthorizedOperation"} {
		t.Run(code, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cause := fmt.Errorf("AWS operation failed: %w", fakeAWSCodedError{code: code})
				calls := 0
				client := fakeSSMInstanceInformationDescriber(func(context.Context, *ssm.DescribeInstanceInformationInput) (*ssm.DescribeInstanceInformationOutput, error) {
					calls++
					return nil, cause
				})
				started := time.Now()
				err := waitForSSMAgentWithClient(context.Background(), client, "i-host", 5*time.Minute)
				if !errors.Is(err, cause) || !strings.Contains(err.Error(), "ssm:DescribeInstanceInformation") {
					t.Fatalf("expected preserved caller error with permission guidance, got %v", err)
				}
				if calls != 1 || time.Since(started) != 0 {
					t.Fatalf("caller failure retried: calls=%d elapsed=%s", calls, time.Since(started))
				}
			})
		})
	}
}

func TestWaitForSSMAgentRetriesUntilOnline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		client := fakeSSMInstanceInformationDescriber(func(_ context.Context, input *ssm.DescribeInstanceInformationInput) (*ssm.DescribeInstanceInformationOutput, error) {
			if len(input.Filters) != 1 || input.Filters[0].Key == nil || *input.Filters[0].Key != "InstanceIds" || len(input.Filters[0].Values) != 1 || input.Filters[0].Values[0] != "i-host" {
				t.Fatalf("expected lookup of only the requested instance, got %#v", input)
			}
			calls++
			switch calls {
			case 1:
				return &ssm.DescribeInstanceInformationOutput{}, nil
			case 2:
				return ssmInformationWithStatus(types.PingStatusConnectionLost), nil
			case 3:
				return nil, fakeAWSCodedError{code: "ThrottlingException"}
			default:
				return ssmInformationWithStatus(types.PingStatusOnline), nil
			}
		})
		if err := waitForSSMAgentWithClient(context.Background(), client, "i-host", time.Minute); err != nil {
			t.Fatalf("expected recovery to Online, got %v", err)
		}
		if calls != 4 {
			t.Fatalf("expected four status checks, got %d", calls)
		}
	})
}

func TestWaitForSSMAgentDeadlineDiagnostics(t *testing.T) {
	apiErr := errors.New("connection reset by peer")
	for _, tt := range []struct {
		name      string
		output    *ssm.DescribeInstanceInformationOutput
		apiErr    error
		block     bool
		wantState string
	}{
		{name: "unregistered", output: &ssm.DescribeInstanceInformationOutput{}, wantState: "not registered with SSM"},
		{name: "offline", output: ssmInformationWithStatus(types.PingStatusConnectionLost), wantState: "PingStatus=ConnectionLost"},
		{name: "API failure", apiErr: apiErr, wantState: "no successful SSM status response"},
		{name: "blocked request", block: true, wantState: "no successful SSM status response"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client := fakeSSMInstanceInformationDescriber(func(ctx context.Context, _ *ssm.DescribeInstanceInformationInput) (*ssm.DescribeInstanceInformationOutput, error) {
					if tt.block {
						<-ctx.Done()
						return nil, ctx.Err()
					}
					// API latency must count toward the overall deadline too.
					time.Sleep(2 * time.Second)
					return tt.output, tt.apiErr
				})
				started := time.Now()
				err := waitForSSMAgentWithClient(context.Background(), client, "i-host", 13*time.Second)
				if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), tt.wantState) {
					t.Fatalf("expected deadline and state %q, got %v", tt.wantState, err)
				}
				if tt.apiErr != nil && !errors.Is(err, tt.apiErr) {
					t.Fatalf("lost underlying API error: %v", err)
				}
				if elapsed := time.Since(started); elapsed != 13*time.Second {
					t.Fatalf("expected actual deadline of 13s, got %s", elapsed)
				}
			})
		})
	}
}

func TestWaitForSSMAgentRetainsAPIErrorWhenDeadlineCancelsNextRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cause := errors.New("SSM endpoint unavailable")
		calls := 0
		client := fakeSSMInstanceInformationDescriber(func(ctx context.Context, _ *ssm.DescribeInstanceInformationInput) (*ssm.DescribeInstanceInformationOutput, error) {
			calls++
			if calls == 1 {
				return nil, cause
			}
			<-ctx.Done()
			return nil, ctx.Err()
		})
		err := waitForSSMAgentWithClient(context.Background(), client, "i-host", 13*time.Second)
		if !errors.Is(err, cause) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected both API error and deadline, got %v", err)
		}
	})
}

func TestWaitForSSMAgentClearsRecoveredAPIError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cause := errors.New("temporary API failure")
		calls := 0
		client := fakeSSMInstanceInformationDescriber(func(context.Context, *ssm.DescribeInstanceInformationInput) (*ssm.DescribeInstanceInformationOutput, error) {
			calls++
			if calls == 1 {
				return nil, cause
			}
			return ssmInformationWithStatus(types.PingStatusConnectionLost), nil
		})
		err := waitForSSMAgentWithClient(context.Background(), client, "i-host", 13*time.Second)
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "PingStatus=ConnectionLost") {
			t.Fatalf("expected offline timeout, got %v", err)
		}
		if errors.Is(err, cause) || strings.Contains(err.Error(), cause.Error()) {
			t.Fatalf("recovered API failure should not be reported as outstanding: %v", err)
		}
	})
}

func TestWaitForSSMAgentHonorsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		client := fakeSSMInstanceInformationDescriber(func(context.Context, *ssm.DescribeInstanceInformationInput) (*ssm.DescribeInstanceInformationOutput, error) {
			cancel()
			return nil, context.Canceled
		})
		started := time.Now()
		err := waitForSSMAgentWithClient(ctx, client, "i-host", time.Minute)
		if !errors.Is(err, context.Canceled) || time.Since(started) != 0 {
			t.Fatalf("expected immediate cancellation, got %v after %s", err, time.Since(started))
		}
	})
}

func TestConfiguredSSMReadyTimeout(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config string
		env    string
		want   time.Duration
		errKey string
	}{
		{name: "default", want: 5 * time.Minute},
		{name: "YAML", config: "120s", want: 2 * time.Minute},
		{name: "environment precedence", config: "2m", env: " 10m ", want: 10 * time.Minute},
		{name: "empty environment", config: "2m", env: " ", want: 2 * time.Minute},
		{name: "missing unit", config: "300", errKey: ssmReadyTimeoutKey},
		{name: "zero", config: "0s", errKey: ssmReadyTimeoutKey},
		{name: "negative", env: "-1m", errKey: ssmReadyTimeoutEnv},
		{name: "invalid environment", config: "5m", env: "invalid", errKey: ssmReadyTimeoutEnv},
		{name: "overflow", env: "9999999999999999999999s", errKey: ssmReadyTimeoutEnv},
	} {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			t.Setenv(ssmReadyTimeoutEnv, tt.env)
			viper.Set(ssmReadyTimeoutKey, tt.config)
			got, err := configuredSSMReadyTimeout()
			if tt.errKey != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errKey) {
					t.Fatalf("expected invalid configuration error naming %s, got %v", tt.errKey, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got (%s, %v), want (%s, nil)", got, err, tt.want)
			}
		})
	}
}

func TestSSMReadyTimeoutPreflightRejectsInvalidConfig(t *testing.T) {
	t.Setenv(ssmReadyTimeoutEnv, "invalid")
	err := validateLocalToolingPreflight(nil)
	if err == nil || !strings.Contains(err.Error(), ssmReadyTimeoutEnv) {
		t.Fatalf("expected timeout config validation before local tooling or provisioning, got %v", err)
	}
}

func TestRunCommandRejectsInvalidSSMReadyTimeout(t *testing.T) {
	t.Setenv(ssmReadyTimeoutEnv, "invalid")
	_, err := RunCommand("true", "192.0.2.1")
	if err == nil || !strings.Contains(err.Error(), ssmReadyTimeoutEnv) {
		t.Fatalf("expected timeout config validation before AWS calls, got %v", err)
	}
}

func ssmInformationWithStatus(status types.PingStatus) *ssm.DescribeInstanceInformationOutput {
	return &ssm.DescribeInstanceInformationOutput{
		InstanceInformationList: []types.InstanceInformation{{PingStatus: status}},
	}
}
