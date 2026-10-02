package test

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/brudnak/ha-rancher-rke2/internal/awspricing"
	"github.com/spf13/viper"
	"log"
	"math"
	"os"
	"strings"
	"time"
)

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
		ec2HourlyRateUSD, err := awspricing.EC2HourlyUSD(region, instanceType)
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
			ebsMonthlyRateUSD, err := awspricing.EBSMonthlyPerGiBUSD(region, volumeType)
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
			estimate.Warnings = append(estimate.Warnings, "RDS/Aurora estimate unavailable: "+err.Error())
		}
	}
	if len(inputs.LoadBalancerDNSNames) > 0 || strings.TrimSpace(inputs.AWSPrefix) != "" || strings.TrimSpace(inputs.RunID) != "" {
		if err := addLoadBalancerCleanupCostEstimate(ctx, estimate, region, inputs, now); err != nil {
			log.Printf("[cleanup] Could not estimate load balancer cost: %v", err)
			estimate.Warnings = append(estimate.Warnings, "Load balancer estimate unavailable: "+err.Error())
		}
	}
	if estimate.InstanceCount == 0 && estimate.VolumeCount == 0 && estimate.DBInstanceCount == 0 && estimate.LoadBalancerCount == 0 {
		return nil, fmt.Errorf("no AWS resources matched cleanup cost estimate inputs")
	}
	return estimate, nil
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
