package test

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2Types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/brudnak/ha-rancher-rke2/internal/awspricing"
	"math"
	"strings"
	"time"
)

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
		rate, err := awspricing.RDSHourlyUSD(region, instanceClass, aws.ToString(db.Engine))
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
		rate, err := awspricing.LoadBalancerHourlyUSD(region, lb.Type)
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
