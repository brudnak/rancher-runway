package test

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/spf13/viper"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func initAWSClients() error {
	if ssmClient != nil {
		return nil
	}

	ctx := context.Background()

	region := viper.GetString("tf_vars.aws_region")
	if region == "" {
		region = viper.GetString("aws.region")
	}
	if region == "" {
		region = "us-east-2"
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				os.Getenv("AWS_SESSION_TOKEN"),
			),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	ssmClient = ssm.NewFromConfig(cfg)
	ec2Client = ec2.NewFromConfig(cfg)
	rdsClient = rds.NewFromConfig(cfg)
	elbv2Client = elasticloadbalancingv2.NewFromConfig(cfg)

	log.Printf("AWS clients initialized for region: %s", region)
	return nil
}

func getInstanceIDFromIP(publicIP string) (string, error) {
	maskGitHubActionsValue(publicIP)
	if err := initAWSClients(); err != nil {
		return "", err
	}

	ctx := context.Background()
	input := &ec2.DescribeInstancesInput{
		Filters: []ec2Types.Filter{
			{
				Name:   aws.String("ip-address"),
				Values: []string{publicIP},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running"},
			},
		},
	}

	result, err := ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to describe instances: %w", err)
	}

	if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
		return "", fmt.Errorf("no running instance found with IP %s", publicIP)
	}

	instanceID := aws.ToString(result.Reservations[0].Instances[0].InstanceId)
	maskGitHubActionsValue(instanceID)
	log.Printf("Resolved IP %s to instance %s", publicIP, instanceID)

	return instanceID, nil
}

func runCommandSSM(cmd string, instanceID string) (string, error) {
	return runCommandSSMWithTimeout(cmd, instanceID, 600, 120)
}

func runCommandSSMWithTimeout(cmd string, instanceID string, executionTimeoutSeconds int32, maxAttempts int) (string, error) {
	maskGitHubActionsValue(instanceID)
	if err := initAWSClients(); err != nil {
		return "", err
	}

	ctx := context.Background()
	log.Printf("[SSM] Sending command to instance %s", instanceID)

	sendInput := &ssm.SendCommandInput{
		InstanceIds:  []string{instanceID},
		DocumentName: aws.String("AWS-RunShellScript"),
		Parameters: map[string][]string{
			"commands":         {cmd},
			"executionTimeout": {strconv.Itoa(int(executionTimeoutSeconds))},
		},
		TimeoutSeconds: aws.Int32(600),
	}

	sendOutput, err := ssmClient.SendCommand(ctx, sendInput)
	if err != nil {
		return "", fmt.Errorf("failed to send SSM command: %w", err)
	}

	commandID := sendOutput.Command.CommandId
	if commandID != nil {
		maskGitHubActionsValue(*commandID)
	}
	log.Printf("[SSM] Command sent with ID: %s", *commandID)

	for i := 0; i < maxAttempts; i++ {
		time.Sleep(5 * time.Second)

		getInput := &ssm.GetCommandInvocationInput{
			CommandId:  commandID,
			InstanceId: aws.String(instanceID),
		}

		getOutput, err := ssmClient.GetCommandInvocation(ctx, getInput)
		if err != nil {
			continue
		}

		status := getOutput.Status

		switch status {
		case types.CommandInvocationStatusSuccess:
			output := aws.ToString(getOutput.StandardOutputContent)
			stderr := aws.ToString(getOutput.StandardErrorContent)

			if stderr != "" {
				log.Printf("[SSM] Command completed with stderr output (%d bytes)", len(stderr))
			}

			trimmedOutput := strings.TrimRight(output, "\r\n")
			log.Printf("[SSM] Command completed successfully. Output length: %d bytes", len(trimmedOutput))
			return trimmedOutput, nil

		case types.CommandInvocationStatusFailed,
			types.CommandInvocationStatusTimedOut,
			types.CommandInvocationStatusCancelled:
			stderr := aws.ToString(getOutput.StandardErrorContent)
			stdout := aws.ToString(getOutput.StandardOutputContent)
			log.Printf("[SSM] Command FAILED with status %s", status)
			log.Printf("[SSM] Failure output sizes: stdout=%d bytes stderr=%d bytes", len(stdout), len(stderr))
			if isRKE2InstallerChecksumFailure(stdout, stderr) {
				log.Printf("[SSM] SECURITY ERROR: RKE2 installer checksum validation failed on remote node")
				return "", fmt.Errorf("remote RKE2 installer checksum validation failed")
			}
			return "", fmt.Errorf("command failed with status %s", status)

		case types.CommandInvocationStatusInProgress,
			types.CommandInvocationStatusPending:
			if i%12 == 0 && i > 0 {
				log.Printf("[SSM] Command still running... (%d seconds)", i*5)
			}
			continue
		}
	}

	getInput := &ssm.GetCommandInvocationInput{
		CommandId:  commandID,
		InstanceId: aws.String(instanceID),
	}
	if getOutput, err := ssmClient.GetCommandInvocation(ctx, getInput); err == nil {
		stdout := aws.ToString(getOutput.StandardOutputContent)
		stderr := aws.ToString(getOutput.StandardErrorContent)
		log.Printf("[SSM] Command polling timed out with remote status %s", getOutput.Status)
		log.Printf("[SSM] Last output sizes: stdout=%d bytes stderr=%d bytes", len(stdout), len(stderr))
	}

	return "", fmt.Errorf("command timed out after %d attempts", maxAttempts)
}

func RunCommand(cmd string, pubIP string) (string, error) {
	return RunCommandWithTimeout(cmd, pubIP, 600, 120)
}

func RunCommandWithTimeout(cmd string, pubIP string, executionTimeoutSeconds int32, maxAttempts int) (string, error) {
	readyTimeout, err := configuredSSMReadyTimeout()
	if err != nil {
		return "", err
	}
	maskGitHubActionsValue(pubIP)
	log.Printf("[RunCommand] Starting command execution for IP %s", pubIP)

	instanceID, err := getInstanceIDFromIP(pubIP)
	if err != nil {
		return "", fmt.Errorf("failed to get instance ID from IP %s: %w", pubIP, err)
	}

	if err := waitForSSMAgent(instanceID, readyTimeout); err != nil {
		return "", fmt.Errorf("SSM agent not ready for instance %s: %w", instanceID, err)
	}

	result, err := runCommandSSMWithTimeout(cmd, instanceID, executionTimeoutSeconds, maxAttempts)
	if err != nil {
		log.Printf("[RunCommand] Command failed: %v", err)
		return "", err
	}

	log.Printf("[RunCommand] Command completed successfully")
	return result, nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
