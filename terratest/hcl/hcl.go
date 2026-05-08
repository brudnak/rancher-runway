package hcl

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

func GenAwsVar(
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
	runID string) {
	GenAwsVarFile(
		"../modules/aws/terraform.tfvars",
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
	)
}

func GenAwsVarFile(
	path,
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
	runID string) {

	f := hclwrite.NewEmptyFile()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Println(err)
		return
	}

	tfVarsFile, err := os.Create(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer tfVarsFile.Close()

	rootBody := f.Body()

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

	_, err = tfVarsFile.Write(f.Bytes())
	if err != nil {
		fmt.Println(err)
		return
	}
}
