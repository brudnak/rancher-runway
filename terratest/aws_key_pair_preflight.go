package test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/spf13/viper"
)

const awsKeyPairPreflightTimeout = 10 * time.Second

type ec2KeyPairDescriber interface {
	DescribeKeyPairs(context.Context, *ec2.DescribeKeyPairsInput, ...func(*ec2.Options)) (*ec2.DescribeKeyPairsOutput, error)
}

type ec2KeyPairDescriberFactory func(context.Context, string) (ec2KeyPairDescriber, error)

type awsErrorCoder interface {
	ErrorCode() string
}

func validateConfiguredAWSEC2KeyPair() error {
	ctx, cancel := context.WithTimeout(context.Background(), awsKeyPairPreflightTimeout)
	defer cancel()

	return validateConfiguredAWSEC2KeyPairWithFactory(ctx, func(ctx context.Context, region string) (ec2KeyPairDescriber, error) {
		cfg, err := awsConfig(ctx, region)
		if err != nil {
			return nil, err
		}
		return ec2.NewFromConfig(cfg), nil
	})
}

func validateConfiguredAWSEC2KeyPairWithFactory(ctx context.Context, factory ec2KeyPairDescriberFactory) error {
	if isLinodeDockerDeployment() {
		return nil
	}

	keyName := strings.TrimSpace(viper.GetString("tf_vars.aws_pem_key_name"))
	region := configuredAWSRegion()
	if keyName == "" {
		return fmt.Errorf("tf_vars.aws_pem_key_name (AWS EC2 key pair name) must be set")
	}
	if factory == nil {
		return fmt.Errorf("could not verify AWS EC2 key pair %q in region %q: EC2 client factory is unavailable", keyName, region)
	}

	client, err := factory(ctx, region)
	if err != nil {
		return formatAWSEC2KeyPairCheckError(ctx, region, keyName, err)
	}
	if err := validateAWSEC2KeyPairWithClient(ctx, client, region, keyName); err != nil {
		return err
	}

	log.Printf("[preflight] AWS EC2 key pair %q exists in region %s", keyName, region)
	return nil
}

func validateAWSEC2KeyPairWithClient(ctx context.Context, client ec2KeyPairDescriber, region, keyName string) error {
	region = strings.TrimSpace(region)
	if region == "" {
		region = "us-east-2"
	}
	keyName = strings.TrimSpace(keyName)
	if keyName == "" {
		return fmt.Errorf("AWS EC2 key pair name must be set")
	}
	if client == nil {
		return fmt.Errorf("could not verify AWS EC2 key pair %q in region %q: EC2 client is unavailable", keyName, region)
	}

	output, err := client.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{
		KeyNames: []string{keyName},
	})
	if err != nil {
		return formatAWSEC2KeyPairCheckError(ctx, region, keyName, err)
	}
	if output != nil {
		for _, keyPair := range output.KeyPairs {
			if strings.TrimSpace(aws.ToString(keyPair.KeyName)) == keyName {
				return nil
			}
		}
	}

	return missingAWSEC2KeyPairError(region, keyName)
}

func formatAWSEC2KeyPairCheckError(ctx context.Context, region, keyName string, err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("timed out verifying AWS EC2 key pair %q in region %q; confirm AWS connectivity and try again", keyName, region)
	}

	var codedErr awsErrorCoder
	if errors.As(err, &codedErr) {
		code := strings.TrimSpace(codedErr.ErrorCode())
		switch {
		case code == "InvalidKeyPair.NotFound":
			return missingAWSEC2KeyPairError(region, keyName)
		case isAWSCredentialOrPermissionError(code):
			return fmt.Errorf("could not verify AWS EC2 key pair %q in region %q because AWS rejected the credentials or ec2:DescribeKeyPairs permission (%s); check the AWS credentials, account, session token, and IAM permissions", keyName, region, code)
		}
	}

	return fmt.Errorf("could not verify AWS EC2 key pair %q in region %q: %w", keyName, region, err)
}

func missingAWSEC2KeyPairError(region, keyName string) error {
	return fmt.Errorf("AWS EC2 key pair %q was not found in region %q; choose an existing key pair in that AWS account and region (enter the EC2 key pair name, not a local .pem file path)", keyName, region)
}

func isAWSCredentialOrPermissionError(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "accessdenied",
		"accessdeniedexception",
		"authfailure",
		"expiredtoken",
		"expiredtokenexception",
		"invalidclienttokenid",
		"unauthorizedoperation",
		"unrecognizedclientexception":
		return true
	default:
		return false
	}
}
