package test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2Types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/spf13/viper"
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

func waitForSSMAgent(instanceID string, maxSeconds int) error {
	maskGitHubActionsValue(instanceID)
	if err := initAWSClients(); err != nil {
		return err
	}

	ctx := context.Background()
	log.Printf("Waiting for SSM agent on %s to be online...", instanceID)

	for i := 0; i < maxSeconds; i++ {
		input := &ssm.DescribeInstanceInformationInput{
			Filters: []types.InstanceInformationStringFilter{
				{
					Key:    aws.String("InstanceIds"),
					Values: []string{instanceID},
				},
			},
		}

		result, err := ssmClient.DescribeInstanceInformation(ctx, input)
		if err == nil && len(result.InstanceInformationList) > 0 {
			status := result.InstanceInformationList[0].PingStatus
			if status == types.PingStatusOnline {
				log.Printf("SSM agent is online for %s", instanceID)
				return nil
			}
		}

		if i%10 == 0 && i > 0 {
			log.Printf("Still waiting for SSM agent... (%d seconds)", i)
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("SSM agent did not come online after %d seconds", maxSeconds)
}

func runCommandSSM(cmd string, instanceID string) (string, error) {
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
			"commands": {cmd},
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

	maxAttempts := 120
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

	return "", fmt.Errorf("command timed out after %d attempts", maxAttempts)
}

func RunCommand(cmd string, pubIP string) (string, error) {
	maskGitHubActionsValue(pubIP)
	log.Printf("[RunCommand] Starting command execution for IP %s", pubIP)

	instanceID, err := getInstanceIDFromIP(pubIP)
	if err != nil {
		return "", fmt.Errorf("failed to get instance ID from IP %s: %w", pubIP, err)
	}

	if err := waitForSSMAgent(instanceID, 120); err != nil {
		return "", fmt.Errorf("SSM agent not ready for instance %s: %w", instanceID, err)
	}

	result, err := runCommandSSM(cmd, instanceID)
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

type cleanupCostEstimateInputs struct {
	InstanceIDs          []string
	DBEndpoints          []string
	LoadBalancerDNSNames []string
	AWSPrefix            string
	RunID                string
}

func estimateCurrentRunCost(totalHAs int, outputs map[string]string) (*cleanupCostEstimate, error) {
	instanceIDs := make([]string, 0, totalHAs*4)
	seenIPs := map[string]bool{}

	for _, ip := range publicServerIPsFromTerraformOutputs(totalHAs, outputs) {
		if ip == "" || seenIPs[ip] {
			continue
		}
		seenIPs[ip] = true

		instanceID, err := getInstanceIDFromIP(ip)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve instance ID for %s: %w", ip, err)
		}
		instanceIDs = append(instanceIDs, instanceID)
	}

	record := readRunRecordForLedger(os.Getenv(runIDEnv))
	region := configuredAWSRegion()
	inputs := cleanupCostEstimateInputs{
		InstanceIDs:          instanceIDs,
		DBEndpoints:          rdsEndpointsFromTerraformOutputs(outputs),
		LoadBalancerDNSNames: loadBalancerDNSNamesFromTerraformOutputs(outputs),
		AWSPrefix:            record.AWSPrefix,
		RunID:                record.RunID,
	}
	if inputs.AWSPrefix == "" {
		inputs.AWSPrefix = terraformAWSPrefixForRun(viper.GetString("tf_vars.aws_prefix"), os.Getenv(runIDEnv))
	}
	if inputs.RunID == "" {
		inputs.RunID = safeRunPathSegment(os.Getenv(runIDEnv))
	}

	return buildCleanupCostEstimate(region, inputs)
}

func estimateCurrentRunCostFromRecordedAWSResources() (*cleanupCostEstimate, error) {
	record := readRunRecordForLedger(os.Getenv(runIDEnv))
	inputs := cleanupCostEstimateInputs{
		AWSPrefix: record.AWSPrefix,
		RunID:     record.RunID,
	}
	if inputs.AWSPrefix == "" {
		inputs.AWSPrefix = terraformAWSPrefixForRun(viper.GetString("tf_vars.aws_prefix"), os.Getenv(runIDEnv))
	}
	if inputs.RunID == "" {
		inputs.RunID = safeRunPathSegment(os.Getenv(runIDEnv))
	}
	if inputs.AWSPrefix == "" && inputs.RunID == "" {
		return nil, fmt.Errorf("no recorded AWS prefix or run id found for cost estimate")
	}
	return buildCleanupCostEstimate(configuredAWSRegion(), inputs)
}

func configuredAWSRegion() string {
	for _, value := range []string{
		viper.GetString("tf_vars.aws_region"),
		viper.GetString("aws.region"),
	} {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return "us-east-2"
}

func publicServerIPsFromTerraformOutputs(totalInstances int, outputs map[string]string) []string {
	ips := make([]string, 0, totalInstances*4)
	for i := 1; i <= totalInstances; i++ {
		haOutputs := getHAOutputs(i, outputs)
		ips = append(ips, haOutputs.ServerIPs...)
		for _, keyPrefix := range []string{
			fmt.Sprintf("hosted_%d_server1_ip", i),
			fmt.Sprintf("hosted_%d_server2_ip", i),
		} {
			if ip := strings.TrimSpace(outputs[keyPrefix]); ip != "" {
				ips = append(ips, ip)
			}
		}
	}
	return ips
}

func rdsEndpointsFromTerraformOutputs(outputs map[string]string) []string {
	endpoints := make([]string, 0)
	for key, value := range outputs {
		if !strings.HasSuffix(key, "_mysql_endpoint") {
			continue
		}
		endpoint := strings.TrimSpace(value)
		if endpoint != "" {
			endpoints = append(endpoints, endpoint)
		}
	}
	return endpoints
}

func loadBalancerDNSNamesFromTerraformOutputs(outputs map[string]string) []string {
	dnsNames := make([]string, 0)
	for key, value := range outputs {
		if !strings.HasSuffix(key, "_aws_lb") {
			continue
		}
		dnsName := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(value, ".")))
		if dnsName != "" {
			dnsNames = append(dnsNames, dnsName)
		}
	}
	return dnsNames
}

func buildCleanupCostEstimate(region string, inputs cleanupCostEstimateInputs) (*cleanupCostEstimate, error) {
	if err := initAWSClients(); err != nil {
		return nil, err
	}

	ctx := context.Background()
	instances, err := instancesForCostEstimate(ctx, inputs)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	totalRuntimeHours := 0.0
	estimatedEC2CostUSD := 0.0
	instanceTypeCounts := map[string]int{}
	volumeIDs := make([]string, 0, len(instances))
	seenVolumes := map[string]bool{}

	for _, instance := range instances {
		instanceType := string(instance.InstanceType)
		instanceTypeCounts[instanceType]++
		ec2HourlyRateUSD, err := lookupEC2OnDemandHourlyPriceUSD(region, instanceType)
		if err != nil {
			return nil, err
		}
		if instance.LaunchTime != nil {
			runtimeHours := now.Sub(*instance.LaunchTime).Hours()
			totalRuntimeHours += runtimeHours
			estimatedEC2CostUSD += ec2HourlyRateUSD * runtimeHours
		}
		for _, mapping := range instance.BlockDeviceMappings {
			if mapping.Ebs == nil || mapping.Ebs.VolumeId == nil {
				continue
			}
			volumeID := aws.ToString(mapping.Ebs.VolumeId)
			if volumeID == "" || seenVolumes[volumeID] {
				continue
			}
			seenVolumes[volumeID] = true
			volumeIDs = append(volumeIDs, volumeID)
		}
	}

	estimatedEBSCostUSD := 0.0
	volumeTypeCounts := map[string]int{}
	volumeSizeGiB := int32(0)
	volumeCount := 0
	if len(volumeIDs) > 0 {
		volumesOutput, err := ec2Client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{
			VolumeIds: volumeIDs,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to describe volumes for cleanup estimate: %w", err)
		}
		volumeCount = len(volumesOutput.Volumes)
		for _, volume := range volumesOutput.Volumes {
			volumeType := string(volume.VolumeType)
			volumeTypeCounts[volumeType]++
			if volumeSizeGiB == 0 {
				volumeSizeGiB = aws.ToInt32(volume.Size)
			}
			ebsMonthlyRateUSD, err := lookupEBSMonthlyPricePerGiBUSD(region, volumeType)
			if err != nil {
				return nil, err
			}
			runtimeHours := totalRuntimeHours / math.Max(float64(len(instances)), 1)
			if volume.CreateTime != nil {
				runtimeHours = now.Sub(*volume.CreateTime).Hours()
			}
			estimatedEBSCostUSD += ebsMonthlyRateUSD * float64(aws.ToInt32(volume.Size)) * (runtimeHours / 730.0)
		}
	}

	estimate := &cleanupCostEstimate{
		Region:              region,
		TotalRuntimeHours:   totalRuntimeHours,
		InstanceCount:       len(instances),
		InstanceType:        summarizeCountedNames(instanceTypeCounts),
		VolumeCount:         volumeCount,
		VolumeType:          summarizeCountedNames(volumeTypeCounts),
		VolumeSizeGiB:       volumeSizeGiB,
		EC2HourlyRateUSD:    estimatedEC2CostUSD / math.Max(totalRuntimeHours, 1),
		EBSMonthlyRateUSD:   0,
		EstimatedEC2CostUSD: estimatedEC2CostUSD,
		EstimatedEBSCostUSD: estimatedEBSCostUSD,
	}

	if len(inputs.DBEndpoints) > 0 || strings.TrimSpace(inputs.AWSPrefix) != "" || strings.TrimSpace(inputs.RunID) != "" {
		if err := addRDSCleanupCostEstimate(ctx, estimate, region, inputs, now); err != nil {
			log.Printf("[cleanup] Could not estimate RDS cost: %v", err)
		}
	}
	if len(inputs.LoadBalancerDNSNames) > 0 || strings.TrimSpace(inputs.AWSPrefix) != "" || strings.TrimSpace(inputs.RunID) != "" {
		if err := addLoadBalancerCleanupCostEstimate(ctx, estimate, region, inputs, now); err != nil {
			log.Printf("[cleanup] Could not estimate load balancer cost: %v", err)
		}
	}
	if estimate.InstanceCount == 0 && estimate.VolumeCount == 0 && estimate.DBInstanceCount == 0 && estimate.LoadBalancerCount == 0 {
		return nil, fmt.Errorf("no AWS resources matched cleanup cost estimate inputs")
	}
	return estimate, nil
}

func instancesForCostEstimate(ctx context.Context, inputs cleanupCostEstimateInputs) ([]ec2Types.Instance, error) {
	if len(inputs.InstanceIDs) > 0 {
		describeOutput, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: inputs.InstanceIDs,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to describe instances for cleanup estimate: %w", err)
		}
		return instancesFromReservations(describeOutput.Reservations), nil
	}

	var filters []ec2Types.Filter
	if prefix := strings.TrimSpace(inputs.AWSPrefix); prefix != "" {
		filters = append(filters, ec2Types.Filter{Name: aws.String("tag:NamePrefix"), Values: []string{prefix}})
	}
	if runID := strings.TrimSpace(inputs.RunID); runID != "" && runID != "unknown" {
		filters = append(filters, ec2Types.Filter{Name: aws.String("tag:HA_Rancher_RKE2_Run_ID"), Values: []string{runID}})
	}
	if len(filters) == 0 {
		return nil, nil
	}
	filters = append(filters, ec2Types.Filter{
		Name:   aws.String("instance-state-name"),
		Values: []string{"pending", "running", "stopping", "stopped"},
	})

	output, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{Filters: filters})
	if err != nil {
		return nil, fmt.Errorf("failed to discover instances for cleanup estimate: %w", err)
	}
	return instancesFromReservations(output.Reservations), nil
}

func instancesFromReservations(reservations []ec2Types.Reservation) []ec2Types.Instance {
	var instances []ec2Types.Instance
	for _, reservation := range reservations {
		instances = append(instances, reservation.Instances...)
	}
	return instances
}

func addRDSCleanupCostEstimate(ctx context.Context, estimate *cleanupCostEstimate, region string, inputs cleanupCostEstimateInputs, now time.Time) error {
	if rdsClient == nil {
		if err := initAWSClients(); err != nil {
			return err
		}
	}
	endpoints := map[string]bool{}
	for _, endpoint := range inputs.DBEndpoints {
		endpoint = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(endpoint, ".")))
		if endpoint != "" {
			endpoints[endpoint] = true
		}
	}
	if len(endpoints) == 0 {
		return nil
	}

	paginator := rds.NewDescribeDBInstancesPaginator(rdsClient, &rds.DescribeDBInstancesInput{})
	var dbs []rdsTypes.DBInstance
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe RDS DB instances: %w", err)
		}
		for _, db := range page.DBInstances {
			address := ""
			if db.Endpoint != nil {
				address = strings.ToLower(strings.TrimSpace(aws.ToString(db.Endpoint.Address)))
			}
			if rdsDBMatchesCostInputs(db, address, endpoints, inputs) {
				dbs = append(dbs, db)
			}
		}
	}
	if len(dbs) == 0 {
		return fmt.Errorf("no RDS DB instances matched Terraform MySQL endpoints or recorded run tags")
	}

	classCounts := map[string]int{}
	totalRDSRuntimeHours := 0.0
	estimatedRDSCostUSD := 0.0
	for _, db := range dbs {
		instanceClass := aws.ToString(db.DBInstanceClass)
		classCounts[aws.ToString(db.DBInstanceClass)]++
		rate, err := lookupRDSHourlyPriceUSD(region, instanceClass, aws.ToString(db.Engine))
		if err != nil {
			return err
		}
		if db.InstanceCreateTime != nil {
			runtimeHours := now.Sub(*db.InstanceCreateTime).Hours()
			totalRDSRuntimeHours += runtimeHours
			estimatedRDSCostUSD += rate * runtimeHours
		}
	}
	estimate.DBInstanceCount = len(dbs)
	estimate.DBInstanceClass = summarizeCountedNames(classCounts)
	estimate.RDSHourlyRateUSD = estimatedRDSCostUSD / math.Max(totalRDSRuntimeHours, 1)
	estimate.EstimatedRDSCostUSD = estimatedRDSCostUSD
	return nil
}

func rdsDBMatchesCostInputs(db rdsTypes.DBInstance, endpointAddress string, endpoints map[string]bool, inputs cleanupCostEstimateInputs) bool {
	if endpointAddress != "" && endpoints[endpointAddress] {
		return true
	}
	dbID := strings.ToLower(aws.ToString(db.DBInstanceIdentifier))
	prefix := strings.ToLower(strings.TrimSpace(inputs.AWSPrefix))
	if prefix != "" && strings.Contains(dbID, prefix) {
		return true
	}
	runID := strings.ToLower(strings.TrimSpace(inputs.RunID))
	return runID != "" && runID != "unknown" && strings.Contains(dbID, runID)
}

func addLoadBalancerCleanupCostEstimate(ctx context.Context, estimate *cleanupCostEstimate, region string, inputs cleanupCostEstimateInputs, now time.Time) error {
	if elbv2Client == nil {
		if err := initAWSClients(); err != nil {
			return err
		}
	}
	loadBalancers, err := loadBalancersForCostEstimate(ctx, inputs)
	if err != nil {
		return err
	}
	if len(loadBalancers) == 0 {
		return fmt.Errorf("no load balancers matched Terraform outputs or recorded run tags")
	}

	typeCounts := map[string]int{}
	totalRuntimeHours := 0.0
	estimatedLBCostUSD := 0.0
	for _, lb := range loadBalancers {
		lbType := string(lb.Type)
		typeCounts[lbType]++
		rate, err := lookupELBLoadBalancerHourlyPriceUSD(region, lb.Type)
		if err != nil {
			return err
		}
		if lb.CreatedTime != nil {
			runtimeHours := now.Sub(*lb.CreatedTime).Hours()
			totalRuntimeHours += runtimeHours
			estimatedLBCostUSD += rate * runtimeHours
		}
	}

	estimate.LoadBalancerCount = len(loadBalancers)
	estimate.LoadBalancerType = summarizeCountedNames(typeCounts)
	estimate.LBHourlyRateUSD = estimatedLBCostUSD / math.Max(totalRuntimeHours, 1)
	estimate.EstimatedLBCostUSD = estimatedLBCostUSD
	return nil
}

func loadBalancersForCostEstimate(ctx context.Context, inputs cleanupCostEstimateInputs) ([]elbv2Types.LoadBalancer, error) {
	dnsNames := map[string]bool{}
	for _, dnsName := range inputs.LoadBalancerDNSNames {
		dnsName = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(dnsName, ".")))
		if dnsName != "" {
			dnsNames[dnsName] = true
		}
	}
	prefix := strings.ToLower(strings.TrimSpace(inputs.AWSPrefix))
	runID := strings.ToLower(strings.TrimSpace(inputs.RunID))

	paginator := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(elbv2Client, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	var matches []elbv2Types.LoadBalancer
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to describe load balancers: %w", err)
		}
		for _, lb := range page.LoadBalancers {
			name := strings.ToLower(aws.ToString(lb.LoadBalancerName))
			dnsName := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(aws.ToString(lb.DNSName), ".")))
			switch {
			case dnsName != "" && dnsNames[dnsName]:
				matches = append(matches, lb)
			case prefix != "" && strings.Contains(name, prefix):
				matches = append(matches, lb)
			case runID != "" && runID != "unknown" && strings.Contains(name, runID):
				matches = append(matches, lb)
			}
		}
	}
	return matches, nil
}

func summarizeCountedNames(counts map[string]int) string {
	if len(counts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(counts))
	for name, count := range counts {
		if count <= 1 {
			parts = append(parts, name)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s x%d", name, count))
	}
	return strings.Join(parts, ", ")
}

func lookupEC2OnDemandHourlyPriceUSD(region, instanceType string) (float64, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				os.Getenv("AWS_SESSION_TOKEN"),
			),
		),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to load AWS pricing config: %w", err)
	}

	pricingClient := pricing.NewFromConfig(cfg)
	location, err := awsPricingLocation(region)
	if err != nil {
		return 0, err
	}

	output, err := pricingClient.GetProducts(context.Background(), &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		MaxResults:  aws.Int32(100),
		Filters: []pricingTypes.Filter{
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("location"), Value: aws.String(location)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("instanceType"), Value: aws.String(instanceType)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("operatingSystem"), Value: aws.String("Linux")},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("tenancy"), Value: aws.String("Shared")},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("preInstalledSw"), Value: aws.String("NA")},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("capacitystatus"), Value: aws.String("Used")},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to query EC2 pricing: %w", err)
	}

	return extractUSDPriceFromPricingResult(output.PriceList)
}

func lookupEBSMonthlyPricePerGiBUSD(region, volumeType string) (float64, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				os.Getenv("AWS_SESSION_TOKEN"),
			),
		),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to load AWS pricing config: %w", err)
	}

	pricingClient := pricing.NewFromConfig(cfg)
	location, err := awsPricingLocation(region)
	if err != nil {
		return 0, err
	}

	output, err := pricingClient.GetProducts(context.Background(), &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		MaxResults:  aws.Int32(100),
		Filters: []pricingTypes.Filter{
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("location"), Value: aws.String(location)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("productFamily"), Value: aws.String("Storage")},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("volumeApiName"), Value: aws.String(volumeType)},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to query EBS pricing: %w", err)
	}

	return extractUSDPriceFromPricingResult(output.PriceList)
}

func lookupRDSHourlyPriceUSD(region, instanceClass, engine string) (float64, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				os.Getenv("AWS_SESSION_TOKEN"),
			),
		),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to load AWS pricing config: %w", err)
	}

	location, err := awsPricingLocation(region)
	if err != nil {
		return 0, err
	}
	databaseEngine := "Aurora MySQL"
	if !strings.Contains(strings.ToLower(engine), "aurora") {
		databaseEngine = "MySQL"
	}

	output, err := pricing.NewFromConfig(cfg).GetProducts(context.Background(), &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonRDS"),
		MaxResults:  aws.Int32(100),
		Filters: []pricingTypes.Filter{
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("location"), Value: aws.String(location)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("instanceType"), Value: aws.String(instanceClass)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("databaseEngine"), Value: aws.String(databaseEngine)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("deploymentOption"), Value: aws.String("Single-AZ")},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to query RDS pricing: %w", err)
	}

	return extractUSDPriceFromPricingResult(output.PriceList)
}

func lookupELBLoadBalancerHourlyPriceUSD(region string, lbType elbv2Types.LoadBalancerTypeEnum) (float64, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				os.Getenv("AWS_SESSION_TOKEN"),
			),
		),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to load AWS pricing config: %w", err)
	}

	location, err := awsPricingLocation(region)
	if err != nil {
		return 0, err
	}

	operation := "LoadBalancing:Application"
	groupDescription := "LoadBalancer hourly usage by Application Load Balancer"
	switch lbType {
	case elbv2Types.LoadBalancerTypeEnumNetwork:
		operation = "LoadBalancing:Network"
		groupDescription = "LoadBalancer hourly usage by Network Load Balancer"
	case elbv2Types.LoadBalancerTypeEnumGateway:
		operation = "LoadBalancing:Gateway"
		groupDescription = "LoadBalancer hourly usage by Gateway Load Balancer"
	}

	output, err := pricing.NewFromConfig(cfg).GetProducts(context.Background(), &pricing.GetProductsInput{
		ServiceCode: aws.String("AWSELB"),
		MaxResults:  aws.Int32(100),
		Filters: []pricingTypes.Filter{
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("location"), Value: aws.String(location)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("operation"), Value: aws.String(operation)},
			{Type: pricingTypes.FilterTypeTermMatch, Field: aws.String("groupDescription"), Value: aws.String(groupDescription)},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to query ELB pricing: %w", err)
	}

	return extractUSDPriceFromPricingResult(output.PriceList)
}

func extractUSDPriceFromPricingResult(priceList []string) (float64, error) {
	type pricingDocument struct {
		Terms struct {
			OnDemand map[string]struct {
				PriceDimensions map[string]struct {
					PricePerUnit map[string]string `json:"pricePerUnit"`
				} `json:"priceDimensions"`
			} `json:"OnDemand"`
		} `json:"terms"`
	}

	bestPrice := math.MaxFloat64
	for _, item := range priceList {
		var doc pricingDocument
		if err := json.Unmarshal([]byte(item), &doc); err != nil {
			continue
		}

		for _, offer := range doc.Terms.OnDemand {
			for _, dimension := range offer.PriceDimensions {
				usdValue := strings.TrimSpace(dimension.PricePerUnit["USD"])
				if usdValue == "" {
					continue
				}
				var price float64
				if _, err := fmt.Sscanf(usdValue, "%f", &price); err != nil {
					continue
				}
				if price > 0 && price < bestPrice {
					bestPrice = price
				}
			}
		}
	}

	if bestPrice == math.MaxFloat64 {
		return 0, fmt.Errorf("no USD price found in pricing response")
	}

	return bestPrice, nil
}

func awsPricingLocation(region string) (string, error) {
	locations := map[string]string{
		"us-east-1": "US East (N. Virginia)",
		"us-east-2": "US East (Ohio)",
		"us-west-1": "US West (N. California)",
		"us-west-2": "US West (Oregon)",
	}
	location := locations[region]
	if location == "" {
		return "", fmt.Errorf("no AWS pricing location mapping configured for region %s", region)
	}
	return location, nil
}

func logCleanupCostEstimate(estimate *cleanupCostEstimate) {
	totalEstimatedUSD := estimate.EstimatedEC2CostUSD + estimate.EstimatedEBSCostUSD + estimate.EstimatedRDSCostUSD + estimate.EstimatedLBCostUSD
	log.Printf("[cleanup] Estimated AWS cost for this run (live pricing):")
	log.Printf("[cleanup] Region: %s", estimate.Region)
	log.Printf("[cleanup] Total runtime across instances: %.2f hours", estimate.TotalRuntimeHours)
	log.Printf("[cleanup] EC2: %d instance(s): %s, blended $%.4f/runtime-hour -> $%.2f estimated",
		estimate.InstanceCount, estimate.InstanceType, estimate.EC2HourlyRateUSD, estimate.EstimatedEC2CostUSD)
	log.Printf("[cleanup] EBS: %d volume(s): %s -> $%.2f estimated",
		estimate.VolumeCount, estimate.VolumeType, estimate.EstimatedEBSCostUSD)
	if estimate.DBInstanceCount > 0 {
		log.Printf("[cleanup] RDS/Aurora: %d DB instance(s): %s, blended $%.4f/runtime-hour -> $%.2f estimated",
			estimate.DBInstanceCount, estimate.DBInstanceClass, estimate.RDSHourlyRateUSD, estimate.EstimatedRDSCostUSD)
	}
	if estimate.LoadBalancerCount > 0 {
		log.Printf("[cleanup] Load balancers: %d load balancer(s): %s, blended $%.4f/runtime-hour -> $%.2f estimated",
			estimate.LoadBalancerCount, estimate.LoadBalancerType, estimate.LBHourlyRateUSD, estimate.EstimatedLBCostUSD)
	}
	log.Printf("[cleanup] Estimated total: $%.2f", totalEstimatedUSD)
}
