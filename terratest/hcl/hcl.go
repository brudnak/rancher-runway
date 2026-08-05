package hcl

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

func GenAwsVar(
	awsRegion,
	awsPrefix,
	awsVpc,
	subnetA,
	subnetB,
	subnetC,
	awsAmi,
	subnetId,
	securityGroupId,
	pemKeyName,
	route53Fqdn,
	customHostnamePrefix,
	ownerFirstName,
	ownerLastName,
	runID,
	deploymentType string,
	totalRancherInstances int,
	awsRDSPassword,
	awsEC2InstanceType string,
	serverCount int) {
	GenAwsVarFile(
		"../modules/aws/terraform.tfvars",
		awsRegion,
		awsPrefix,
		awsVpc,
		subnetA,
		subnetB,
		subnetC,
		awsAmi,
		subnetId,
		securityGroupId,
		pemKeyName,
		route53Fqdn,
		customHostnamePrefix,
		ownerFirstName,
		ownerLastName,
		runID,
		deploymentType,
		totalRancherInstances,
		awsRDSPassword,
		awsEC2InstanceType,
		serverCount,
	)
}

func GenAwsVarFile(
	path,
	awsRegion,
	awsPrefix,
	awsVpc,
	subnetA,
	subnetB,
	subnetC,
	awsAmi,
	subnetId,
	securityGroupId,
	pemKeyName,
	route53Fqdn,
	customHostnamePrefix,
	ownerFirstName,
	ownerLastName,
	runID,
	deploymentType string,
	totalRancherInstances int,
	awsRDSPassword,
	awsEC2InstanceType string,
	serverCount int) {

	f := hclwrite.NewEmptyFile()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Println(err)
		return
	}

	rootBody := f.Body()

	rootBody.SetAttributeValue("aws_region", cty.StringVal(awsRegion))
	rootBody.SetAttributeValue("aws_prefix", cty.StringVal(awsPrefix))
	rootBody.SetAttributeValue("aws_vpc", cty.StringVal(awsVpc))
	rootBody.SetAttributeValue("aws_subnet_a", cty.StringVal(subnetA))
	rootBody.SetAttributeValue("aws_subnet_b", cty.StringVal(subnetB))
	rootBody.SetAttributeValue("aws_subnet_c", cty.StringVal(subnetC))
	rootBody.SetAttributeValue("aws_ami", cty.StringVal(awsAmi))
	rootBody.SetAttributeValue("aws_subnet_id", cty.StringVal(subnetId))
	rootBody.SetAttributeValue("aws_security_group_id", cty.StringVal(securityGroupId))
	rootBody.SetAttributeValue("aws_pem_key_name", cty.StringVal(pemKeyName))
	rootBody.SetAttributeValue("aws_route53_fqdn", cty.StringVal(route53Fqdn))
	rootBody.SetAttributeValue("custom_hostname_prefix", cty.StringVal(customHostnamePrefix))
	rootBody.SetAttributeValue("owner_first_name", cty.StringVal(ownerFirstName))
	rootBody.SetAttributeValue("owner_last_name", cty.StringVal(ownerLastName))
	rootBody.SetAttributeValue("run_id", cty.StringVal(runID))
	rootBody.SetAttributeValue("deployment_type", cty.StringVal(deploymentType))
	rootBody.SetAttributeValue("total_rancher_instances", cty.NumberIntVal(int64(totalRancherInstances)))
	rootBody.SetAttributeValue("aws_rds_password", cty.StringVal(awsRDSPassword))
	rootBody.SetAttributeValue("aws_ec2_instance_type", cty.StringVal(awsEC2InstanceType))
	rootBody.SetAttributeValue("server_count", cty.NumberIntVal(int64(serverCount)))

	if err := writePrivateFileAtomically(path, f.Bytes()); err != nil {
		fmt.Println(err)
	}
}

func writePrivateFileAtomically(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".terraform-tfvars-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
