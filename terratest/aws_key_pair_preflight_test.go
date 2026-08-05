package test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/spf13/viper"
)

type fakeEC2KeyPairDescriber struct {
	input  *ec2.DescribeKeyPairsInput
	output *ec2.DescribeKeyPairsOutput
	err    error
}

func (f *fakeEC2KeyPairDescriber) DescribeKeyPairs(_ context.Context, input *ec2.DescribeKeyPairsInput, _ ...func(*ec2.Options)) (*ec2.DescribeKeyPairsOutput, error) {
	f.input = input
	return f.output, f.err
}

type fakeAWSCodedError struct {
	code string
}

func (e fakeAWSCodedError) Error() string {
	return e.code
}

func (e fakeAWSCodedError) ErrorCode() string {
	return e.code
}

func TestValidateAWSEC2KeyPairWithClientFindsExactKey(t *testing.T) {
	client := &fakeEC2KeyPairDescriber{output: &ec2.DescribeKeyPairsOutput{
		KeyPairs: []ec2Types.KeyPairInfo{{KeyName: aws.String("qa-key")}},
	}}

	if err := validateAWSEC2KeyPairWithClient(context.Background(), client, "us-west-2", " qa-key "); err != nil {
		t.Fatalf("validateAWSEC2KeyPairWithClient returned error: %v", err)
	}
	if client.input == nil || len(client.input.KeyNames) != 1 || client.input.KeyNames[0] != "qa-key" {
		t.Fatalf("expected exact key-name lookup, got %#v", client.input)
	}
}

func TestValidateAWSEC2KeyPairWithClientReportsMissingKeyClearly(t *testing.T) {
	client := &fakeEC2KeyPairDescriber{output: &ec2.DescribeKeyPairsOutput{}}

	err := validateAWSEC2KeyPairWithClient(context.Background(), client, "us-east-1", "ur-pem-key")
	if err == nil {
		t.Fatal("expected missing key pair to fail")
	}
	for _, expected := range []string{"ur-pem-key", "us-east-1", "not found", "not a local .pem file path"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected missing-key error to contain %q, got %q", expected, err)
		}
	}
}

func TestValidateAWSEC2KeyPairWithClientClassifiesAWSNotFound(t *testing.T) {
	client := &fakeEC2KeyPairDescriber{err: fakeAWSCodedError{code: "InvalidKeyPair.NotFound"}}

	err := validateAWSEC2KeyPairWithClient(context.Background(), client, "us-east-2", "missing-key")
	if err == nil || !strings.Contains(err.Error(), "was not found") {
		t.Fatalf("expected friendly not-found error, got %v", err)
	}
}

func TestValidateAWSEC2KeyPairWithClientClassifiesPermissionError(t *testing.T) {
	client := &fakeEC2KeyPairDescriber{err: fakeAWSCodedError{code: "UnauthorizedOperation"}}

	err := validateAWSEC2KeyPairWithClient(context.Background(), client, "us-east-2", "qa-key")
	if err == nil {
		t.Fatal("expected permission error")
	}
	for _, expected := range []string{"credentials", "ec2:DescribeKeyPairs", "UnauthorizedOperation"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected permission error to contain %q, got %q", expected, err)
		}
	}
}

func TestValidateAWSEC2KeyPairWithClientClassifiesTimeout(t *testing.T) {
	client := &fakeEC2KeyPairDescriber{err: context.DeadlineExceeded}

	err := validateAWSEC2KeyPairWithClient(context.Background(), client, "us-east-2", "qa-key")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout guidance, got %v", err)
	}
}

func TestValidateAWSEC2KeyPairWithClientPreservesUnexpectedFailure(t *testing.T) {
	client := &fakeEC2KeyPairDescriber{err: errors.New("connection reset")}

	err := validateAWSEC2KeyPairWithClient(context.Background(), client, "us-east-2", "qa-key")
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("expected underlying error context, got %v", err)
	}
}

func TestValidateConfiguredAWSEC2KeyPairUsesConfiguredRegion(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("deployment.type", deploymentTypeHARKE2)
	viper.Set("tf_vars.aws_region", "us-west-1")
	viper.Set("tf_vars.aws_pem_key_name", "qa-key")

	var factoryRegion string
	client := &fakeEC2KeyPairDescriber{output: &ec2.DescribeKeyPairsOutput{
		KeyPairs: []ec2Types.KeyPairInfo{{KeyName: aws.String("qa-key")}},
	}}
	err := validateConfiguredAWSEC2KeyPairWithFactory(context.Background(), func(_ context.Context, region string) (ec2KeyPairDescriber, error) {
		factoryRegion = region
		return client, nil
	})
	if err != nil {
		t.Fatalf("validateConfiguredAWSEC2KeyPairWithFactory returned error: %v", err)
	}
	if factoryRegion != "us-west-1" {
		t.Fatalf("factory region = %q, want us-west-1", factoryRegion)
	}
}

func TestValidateConfiguredAWSEC2KeyPairSkipsLinode(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("deployment.type", deploymentTypeLinodeDocker)

	factoryCalled := false
	err := validateConfiguredAWSEC2KeyPairWithFactory(context.Background(), func(_ context.Context, _ string) (ec2KeyPairDescriber, error) {
		factoryCalled = true
		return nil, errors.New("must not be called")
	})
	if err != nil {
		t.Fatalf("expected Linode deployment to skip EC2 key-pair check, got %v", err)
	}
	if factoryCalled {
		t.Fatal("expected Linode deployment not to create an EC2 client")
	}
}

func TestTerraformVarsUsesConfiguredAWSRegion(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("deployment.type", deploymentTypeHARKE2)
	viper.Set("tf_vars.aws_region", "eu-west-1")

	vars := terraformVars(1, "")
	if got := vars["aws_region"]; got != "eu-west-1" {
		t.Fatalf("terraform aws_region = %#v, want eu-west-1", got)
	}
}
