package test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/spf13/viper"
)

const (
	defaultSSMReadyTimeout = 5 * time.Minute
	ssmReadyTimeoutEnv     = "RUNWAY_SSM_READY_TIMEOUT"
	ssmReadyTimeoutKey     = "aws.ssm_ready_timeout"
	ssmReadyPollInterval   = 5 * time.Second
)

type ssmInstanceInformationDescriber interface {
	DescribeInstanceInformation(context.Context, *ssm.DescribeInstanceInformationInput, ...func(*ssm.Options)) (*ssm.DescribeInstanceInformationOutput, error)
}

func configuredSSMReadyTimeout() (time.Duration, error) {
	source := ssmReadyTimeoutEnv
	value := strings.TrimSpace(os.Getenv(source))
	if value == "" {
		source = ssmReadyTimeoutKey
		value = strings.TrimSpace(viper.GetString(source))
	}
	if value == "" {
		return defaultSSMReadyTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as 5m or 300s", source)
	}
	return timeout, nil
}

func waitForSSMAgent(instanceID string, timeout time.Duration) error {
	maskGitHubActionsValue(instanceID)
	if err := initAWSClients(); err != nil {
		return err
	}
	return waitForSSMAgentWithClient(context.Background(), ssmClient, instanceID, timeout)
}

func waitForSSMAgentWithClient(ctx context.Context, client ssmInstanceInformationDescriber, instanceID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(ssmReadyPollInterval)
	defer ticker.Stop()
	started := time.Now()
	nextLog := started.Add(10 * time.Second)
	lastState := "no successful SSM status response"
	var lastErr error
	log.Printf("Waiting for SSM agent on %s to be online (timeout %s)...", instanceID, timeout)

	input := &ssm.DescribeInstanceInformationInput{
		Filters: []types.InstanceInformationStringFilter{{
			Key: aws.String("InstanceIds"), Values: []string{instanceID},
		}},
	}
	for ctx.Err() == nil {
		result, err := client.DescribeInstanceInformation(ctx, input)
		if ctx.Err() != nil {
			// Keep the last useful API error when the deadline cancels a request.
			break
		}
		if err != nil {
			lastErr = err
			var codedErr awsErrorCoder
			if errors.As(err, &codedErr) && isAWSCredentialOrPermissionError(codedErr.ErrorCode()) {
				return fmt.Errorf("SSM DescribeInstanceInformation failed for %s: AWS rejected the caller credentials or ssm:DescribeInstanceInformation permission; check the CI role or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY/AWS_SESSION_TOKEN used by Runway: %w", instanceID, err)
			}
		} else {
			lastErr = nil
			lastState = "instance is not registered with SSM"
			if result != nil && len(result.InstanceInformationList) > 0 {
				status := result.InstanceInformationList[0].PingStatus
				lastState = "PingStatus=" + string(status)
				if status == types.PingStatusOnline {
					log.Printf("SSM agent is online for %s", instanceID)
					return nil
				}
			}
		}

		if !time.Now().Before(nextLog) {
			log.Printf("Still waiting for SSM agent on %s (%s elapsed; last state: %s)", instanceID, time.Since(started).Round(time.Second), lastState)
			if lastErr != nil {
				log.Printf("[SSM] Last DescribeInstanceInformation error: %v", lastErr)
			}
			nextLog = time.Now().Add(10 * time.Second)
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}

	cause := ctx.Err()
	if lastErr != nil {
		cause = errors.Join(cause, fmt.Errorf("last DescribeInstanceInformation error: %w", lastErr))
	}
	return fmt.Errorf("waiting for SSM agent on %s stopped after %s (last state: %s); check agent startup, the EC2 instance profile, and SSM endpoint connectivity: %w", instanceID, time.Since(started).Round(time.Second), lastState, cause)
}
