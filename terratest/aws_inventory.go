package test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbTypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamTypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/spf13/viper"
)

const awsManagedByTagValue = "ha-rancher-rke2"

func (p *localControlPanel) discoverAWSInventory(records []panelRunRecord) panelAWSInventoryState {
	region := strings.TrimSpace(viper.GetString("tf_vars.aws_region"))
	if region == "" {
		region = "us-east-2"
	}
	owner := normalizeAWSOwner(viper.GetString("user.first_name") + " " + viper.GetString("user.last_name"))
	prefixes, runByPrefix := p.awsInventoryPrefixes(records, owner)
	cacheKey := strings.Join(append([]string{region, owner}, prefixes...), "\x00")

	p.awsMu.Lock()
	if p.awsCacheKey == cacheKey && !p.awsCache.UpdatedAt.IsZero() && time.Since(p.awsCache.UpdatedAt) < 30*time.Second {
		cached := p.awsCache
		p.awsMu.Unlock()
		return cached
	}
	p.awsMu.Unlock()

	state := panelAWSInventoryState{
		UpdatedAt: time.Now(),
		Region:    region,
		Owner:     owner,
		Queries:   awsInventoryQueryLabels(prefixes, owner),
		Items:     []awsResourceView{},
	}

	if len(prefixes) == 0 && strings.TrimSpace(owner) == "" {
		state.Error = "AWS inventory needs either recorded run slots or user.first_name/user.last_name owner tags."
		p.cacheAWSInventory(cacheKey, state)
		return state
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	cfg, err := awsConfig(ctx, region)
	if err != nil {
		state.Error = err.Error()
		p.cacheAWSInventory(cacheKey, state)
		return state
	}

	collector := &awsInventoryCollector{
		state:       &state,
		region:      region,
		owner:       owner,
		prefixes:    prefixes,
		runByPrefix: runByPrefix,
		seen:        map[string]bool{},
	}

	collector.collectEC2(ctx, ec2.NewFromConfig(cfg))
	collector.collectELB(ctx, elasticloadbalancingv2.NewFromConfig(cfg))
	collector.collectIAM(ctx, iam.NewFromConfig(cfg))
	collector.collectACM(ctx, acm.NewFromConfig(cfg))
	collector.collectRoute53(ctx, route53.NewFromConfig(cfg), records)

	sort.Slice(state.Items, func(i, j int) bool {
		if state.Items[i].Type != state.Items[j].Type {
			return state.Items[i].Type < state.Items[j].Type
		}
		if state.Items[i].Name != state.Items[j].Name {
			return state.Items[i].Name < state.Items[j].Name
		}
		return state.Items[i].ID < state.Items[j].ID
	})
	if len(collector.errors) > 0 {
		state.Error = strings.Join(collector.errors, "; ")
	}
	p.cacheAWSInventory(cacheKey, state)
	return state
}

func (p *localControlPanel) cacheAWSInventory(cacheKey string, state panelAWSInventoryState) {
	p.awsMu.Lock()
	defer p.awsMu.Unlock()
	p.awsCacheKey = cacheKey
	p.awsCache = state
}

func (p *localControlPanel) awsInventoryPrefixes(records []panelRunRecord, owner string) ([]string, map[string]string) {
	runByPrefix := map[string]string{}
	add := func(prefix, runID string) {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			return
		}
		runByPrefix[prefix] = safeRunPathSegment(runID)
	}
	for _, record := range records {
		if !awsInventoryRecordMatchesOwner(record, owner) {
			continue
		}
		add(record.AWSPrefix, record.RunID)
	}

	p.mu.Lock()
	for _, op := range p.operations {
		if op == nil || strings.TrimSpace(op.RunID) == "" {
			continue
		}
		if op.FinishedAt != nil && time.Since(*op.FinishedAt) > time.Hour {
			continue
		}
		add(terraformAWSPrefixForRun(viper.GetString("tf_vars.aws_prefix"), op.RunID), op.RunID)
	}
	p.mu.Unlock()

	prefixes := make([]string, 0, len(runByPrefix))
	for prefix := range runByPrefix {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)
	return prefixes, runByPrefix
}

func awsInventoryRecordMatchesOwner(record panelRunRecord, owner string) bool {
	owner = normalizeAWSOwner(owner)
	if owner == "" {
		return true
	}
	recordOwner := normalizeAWSOwner(record.Owner)
	return recordOwner == "" || recordOwner == owner
}

func awsInventoryQueryLabels(prefixes []string, owner string) []string {
	labels := make([]string, 0, len(prefixes)+1)
	for _, prefix := range prefixes {
		labels = append(labels, "name/tag prefix "+prefix)
	}
	if strings.TrimSpace(owner) != "" {
		labels = append(labels, "Owner tag "+strings.Join(strings.Fields(owner), " "))
	}
	return labels
}

func normalizeAWSOwner(owner string) string {
	return strings.Join(strings.Fields(owner), " ")
}

func awsConfig(ctx context.Context, region string) (aws.Config, error) {
	return config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				configuredAWSValue("aws.access_key_id", "AWS_ACCESS_KEY_ID"),
				getenvFallback("AWS_SECRET_ACCESS_KEY"),
				getenvFallback("AWS_SESSION_TOKEN"),
			),
		),
	)
}

func getenvFallback(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func configuredAWSValue(configKey, envKey string) string {
	if value := strings.TrimSpace(viper.GetString(configKey)); value != "" {
		return value
	}
	return getenvFallback(envKey)
}

type awsInventoryCollector struct {
	state       *panelAWSInventoryState
	region      string
	owner       string
	prefixes    []string
	runByPrefix map[string]string
	seen        map[string]bool
	errors      []string
}

func (c *awsInventoryCollector) collectEC2(ctx context.Context, client *ec2.Client) {
	instanceFilters := []ec2Types.Filter{
		{Name: aws.String("instance-state-name"), Values: []string{"pending", "running", "stopping", "stopped", "shutting-down"}},
	}
	for _, filter := range c.matchFilters("tag:Name") {
		input := &ec2.DescribeInstancesInput{Filters: append(append([]ec2Types.Filter{}, instanceFilters...), filter)}
		paginator := ec2.NewDescribeInstancesPaginator(client, input)
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				c.addError("EC2 instances: %v", err)
				break
			}
			for _, reservation := range page.Reservations {
				for _, instance := range reservation.Instances {
					tags := ec2Tags(instance.Tags)
					name := tags["Name"]
					if !c.matches(name, tags) {
						continue
					}
					c.add(awsResourceView{
						Type:    "EC2 instance",
						ID:      aws.ToString(instance.InstanceId),
						Name:    name,
						Region:  c.region,
						Status:  string(instance.State.Name),
						RunID:   c.runID(name, tags),
						Owner:   tags["Owner"],
						Source:  "ec2:DescribeInstances",
						Details: string(instance.InstanceType),
						Tags:    tags,
					})
				}
			}
		}
	}

	for _, filter := range c.matchFilters("tag:Name") {
		paginator := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{Filters: []ec2Types.Filter{filter}})
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				c.addError("EBS volumes: %v", err)
				break
			}
			for _, volume := range page.Volumes {
				tags := ec2Tags(volume.Tags)
				name := tags["Name"]
				if !c.matches(name, tags) {
					continue
				}
				c.add(awsResourceView{
					Type:    "EBS volume",
					ID:      aws.ToString(volume.VolumeId),
					Name:    name,
					Region:  c.region,
					Status:  string(volume.State),
					RunID:   c.runID(name, tags),
					Owner:   tags["Owner"],
					Source:  "ec2:DescribeVolumes",
					Details: fmt.Sprintf("%d GiB %s", aws.ToInt32(volume.Size), volume.VolumeType),
					Tags:    tags,
				})
			}
		}
	}
}

func (c *awsInventoryCollector) collectELB(ctx context.Context, client *elasticloadbalancingv2.Client) {
	lbPaginator := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(client, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	for lbPaginator.HasMorePages() {
		page, err := lbPaginator.NextPage(ctx)
		if err != nil {
			c.addError("load balancers: %v", err)
			break
		}
		for _, lb := range page.LoadBalancers {
			arn := aws.ToString(lb.LoadBalancerArn)
			tags := c.elbTags(ctx, client, arn)
			name := aws.ToString(lb.LoadBalancerName)
			if !c.matches(name, tags) {
				continue
			}
			c.add(awsResourceView{
				Type:    "ALB",
				ID:      arn,
				Name:    name,
				Region:  c.region,
				Status:  string(lb.State.Code),
				RunID:   c.runID(name, tags),
				Owner:   tags["Owner"],
				Source:  "elasticloadbalancing:DescribeLoadBalancers",
				Details: aws.ToString(lb.DNSName),
				Tags:    tags,
			})
			c.collectELBListeners(ctx, client, arn, name, tags)
		}
	}

	tgPaginator := elasticloadbalancingv2.NewDescribeTargetGroupsPaginator(client, &elasticloadbalancingv2.DescribeTargetGroupsInput{})
	for tgPaginator.HasMorePages() {
		page, err := tgPaginator.NextPage(ctx)
		if err != nil {
			c.addError("target groups: %v", err)
			break
		}
		for _, tg := range page.TargetGroups {
			arn := aws.ToString(tg.TargetGroupArn)
			tags := c.elbTags(ctx, client, arn)
			name := aws.ToString(tg.TargetGroupName)
			if !c.matches(name, tags) {
				continue
			}
			c.add(awsResourceView{
				Type:    "Target group",
				ID:      arn,
				Name:    name,
				Region:  c.region,
				Status:  string(tg.TargetType),
				RunID:   c.runID(name, tags),
				Owner:   tags["Owner"],
				Source:  "elasticloadbalancing:DescribeTargetGroups",
				Details: fmt.Sprintf("%s:%d", tg.Protocol, aws.ToInt32(tg.Port)),
				Tags:    tags,
			})
		}
	}
}

func (c *awsInventoryCollector) collectELBListeners(ctx context.Context, client *elasticloadbalancingv2.Client, lbARN, lbName string, inheritedTags map[string]string) {
	paginator := elasticloadbalancingv2.NewDescribeListenersPaginator(client, &elasticloadbalancingv2.DescribeListenersInput{
		LoadBalancerArn: aws.String(lbARN),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			c.addError("listeners for %s: %v", lbName, err)
			return
		}
		for _, listener := range page.Listeners {
			arn := aws.ToString(listener.ListenerArn)
			tags := c.elbTags(ctx, client, arn)
			if len(tags) == 0 {
				tags = inheritedTags
			}
			name := fmt.Sprintf("%s:%d", lbName, aws.ToInt32(listener.Port))
			if !c.matches(name, tags) {
				continue
			}
			c.add(awsResourceView{
				Type:    "ALB listener",
				ID:      arn,
				Name:    name,
				Region:  c.region,
				Status:  string(listener.Protocol),
				RunID:   c.runID(lbName, tags),
				Owner:   tags["Owner"],
				Source:  "elasticloadbalancing:DescribeListeners",
				Details: string(listener.Protocol),
				Tags:    tags,
			})
		}
	}
}

func (c *awsInventoryCollector) collectIAM(ctx context.Context, client *iam.Client) {
	rolePaginator := iam.NewListRolesPaginator(client, &iam.ListRolesInput{})
	for rolePaginator.HasMorePages() {
		page, err := rolePaginator.NextPage(ctx)
		if err != nil {
			c.addError("IAM roles: %v", err)
			break
		}
		for _, role := range page.Roles {
			name := aws.ToString(role.RoleName)
			tags := c.iamRoleTags(ctx, client, name)
			if !c.matches(name, tags) {
				continue
			}
			runID := c.runID(name, tags)
			c.add(awsResourceView{
				Type:    "IAM role",
				ID:      aws.ToString(role.Arn),
				Name:    name,
				Region:  "global",
				RunID:   runID,
				Owner:   tags["Owner"],
				Source:  "iam:ListRoles",
				Details: aws.ToString(role.Path),
				Tags:    tags,
			})
			c.collectIAMRolePolicyAttachments(ctx, client, name, aws.ToString(role.Arn), runID, tags)
		}
	}

	profilePaginator := iam.NewListInstanceProfilesPaginator(client, &iam.ListInstanceProfilesInput{})
	for profilePaginator.HasMorePages() {
		page, err := profilePaginator.NextPage(ctx)
		if err != nil {
			c.addError("IAM instance profiles: %v", err)
			break
		}
		for _, profile := range page.InstanceProfiles {
			name := aws.ToString(profile.InstanceProfileName)
			tags := c.iamInstanceProfileTags(ctx, client, name)
			if !c.matches(name, tags) {
				continue
			}
			c.add(awsResourceView{
				Type:    "IAM instance profile",
				ID:      aws.ToString(profile.Arn),
				Name:    name,
				Region:  "global",
				RunID:   c.runID(name, tags),
				Owner:   tags["Owner"],
				Source:  "iam:ListInstanceProfiles",
				Details: aws.ToString(profile.Path),
				Tags:    tags,
			})
		}
	}
}

func (c *awsInventoryCollector) collectIAMRolePolicyAttachments(ctx context.Context, client *iam.Client, roleName, roleARN, runID string, roleTags map[string]string) {
	paginator := iam.NewListAttachedRolePoliciesPaginator(client, &iam.ListAttachedRolePoliciesInput{RoleName: aws.String(roleName)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			c.addError("IAM role policy attachments for %s: %v", roleName, err)
			return
		}
		for _, policy := range page.AttachedPolicies {
			policyName := aws.ToString(policy.PolicyName)
			policyARN := aws.ToString(policy.PolicyArn)
			c.add(awsResourceView{
				Type:    "IAM policy attachment",
				ID:      roleARN + ":" + policyARN,
				Name:    policyName,
				Region:  "global",
				RunID:   runID,
				Owner:   roleTags["Owner"],
				Source:  "iam:ListAttachedRolePolicies",
				Details: fmt.Sprintf("%s -> %s", roleName, policyARN),
				Tags:    roleTags,
			})
		}
	}
}

func (c *awsInventoryCollector) collectACM(ctx context.Context, client *acm.Client) {
	paginator := acm.NewListCertificatesPaginator(client, &acm.ListCertificatesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			c.addError("ACM certificates: %v", err)
			break
		}
		for _, cert := range page.CertificateSummaryList {
			arn := aws.ToString(cert.CertificateArn)
			tags := c.acmTags(ctx, client, arn)
			name := aws.ToString(cert.DomainName)
			if !c.matches(name, tags) {
				continue
			}
			c.add(awsResourceView{
				Type:   "ACM certificate",
				ID:     arn,
				Name:   name,
				Region: c.region,
				Status: string(cert.Status),
				RunID:  c.runID(name, tags),
				Owner:  tags["Owner"],
				Source: "acm:ListCertificates",
				Tags:   tags,
			})
		}
	}
}

func (c *awsInventoryCollector) collectRoute53(ctx context.Context, client *route53.Client, records []panelRunRecord) {
	zoneNames := map[string]bool{}
	for _, record := range records {
		if strings.TrimSpace(record.Route53FQDN) != "" {
			zoneNames[strings.TrimSuffix(strings.TrimSpace(record.Route53FQDN), ".")+"."] = true
		}
	}
	for zoneName := range zoneNames {
		zones, err := client.ListHostedZonesByName(ctx, &route53.ListHostedZonesByNameInput{DNSName: aws.String(zoneName)})
		if err != nil {
			c.addError("Route53 zone %s: %v", zoneName, err)
			continue
		}
		for _, zone := range zones.HostedZones {
			if aws.ToString(zone.Name) != zoneName {
				continue
			}
			c.collectRoute53Records(ctx, client, aws.ToString(zone.Id), zoneName)
		}
	}
}

func (c *awsInventoryCollector) collectRoute53Records(ctx context.Context, client *route53.Client, zoneID, zoneName string) {
	paginator := route53.NewListResourceRecordSetsPaginator(client, &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(zoneID),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			c.addError("Route53 records %s: %v", zoneName, err)
			break
		}
		for _, record := range page.ResourceRecordSets {
			name := strings.TrimSuffix(aws.ToString(record.Name), ".")
			if !c.nameHasPrefix(name) && !c.route53ValidationNameMatches(name) {
				continue
			}
			c.add(awsResourceView{
				Type:    "Route53 record",
				ID:      zoneID + ":" + name + ":" + string(record.Type),
				Name:    name,
				Region:  "global",
				Status:  string(record.Type),
				RunID:   c.runID(name, nil),
				Source:  "route53:ListResourceRecordSets",
				Details: fmt.Sprintf("%d target(s)", len(record.ResourceRecords)),
			})
		}
	}
}

func (c *awsInventoryCollector) matchFilters(tagName string) []ec2Types.Filter {
	filters := make([]ec2Types.Filter, 0, len(c.prefixes)+1)
	for _, prefix := range c.prefixes {
		filters = append(filters, ec2Types.Filter{
			Name:   aws.String(tagName),
			Values: []string{prefix + "*"},
		})
	}
	if c.owner != "" {
		filters = append(filters, ec2Types.Filter{
			Name:   aws.String("tag:Owner"),
			Values: []string{c.owner},
		})
	}
	return filters
}

func (c *awsInventoryCollector) matches(name string, tags map[string]string) bool {
	if !c.ownerAllows(tags) {
		return false
	}
	return c.nameHasPrefix(name) || c.tagsMatch(tags)
}

func (c *awsInventoryCollector) ownerAllows(tags map[string]string) bool {
	if c.owner == "" || len(tags) == 0 {
		return true
	}
	resourceOwner := normalizeAWSOwner(tags["Owner"])
	return resourceOwner == "" || resourceOwner == c.owner
}

func (c *awsInventoryCollector) nameHasPrefix(name string) bool {
	for _, prefix := range c.prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (c *awsInventoryCollector) tagsMatch(tags map[string]string) bool {
	if len(tags) == 0 {
		return false
	}
	if c.owner != "" {
		return normalizeAWSOwner(tags["Owner"]) == c.owner
	}
	return tags["ManagedBy"] == awsManagedByTagValue
}

func (c *awsInventoryCollector) route53ValidationNameMatches(name string) bool {
	for _, prefix := range c.prefixes {
		if strings.Contains(name, "."+prefix+".") || strings.Contains(name, "_"+prefix+".") {
			return true
		}
	}
	return false
}

func (c *awsInventoryCollector) runID(name string, tags map[string]string) string {
	if runID := strings.TrimSpace(tags["HA_Rancher_RKE2_Run_ID"]); runID != "" {
		return safeRunPathSegment(runID)
	}
	for prefix, runID := range c.runByPrefix {
		if strings.HasPrefix(name, prefix) {
			return runID
		}
	}
	return ""
}

func (c *awsInventoryCollector) add(item awsResourceView) {
	if item.ID == "" {
		item.ID = item.Type + ":" + item.Name
	}
	key := item.Type + "\x00" + item.ID
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	if item.Tags != nil && len(item.Tags) == 0 {
		item.Tags = nil
	}
	c.state.Items = append(c.state.Items, item)
}

func (c *awsInventoryCollector) addError(format string, args ...interface{}) {
	c.errors = append(c.errors, fmt.Sprintf(format, args...))
}

func (c *awsInventoryCollector) elbTags(ctx context.Context, client *elasticloadbalancingv2.Client, arn string) map[string]string {
	if arn == "" {
		return nil
	}
	out, err := client.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{ResourceArns: []string{arn}})
	if err != nil || len(out.TagDescriptions) == 0 {
		return nil
	}
	return elbTags(out.TagDescriptions[0].Tags)
}

func (c *awsInventoryCollector) iamRoleTags(ctx context.Context, client *iam.Client, name string) map[string]string {
	out, err := client.ListRoleTags(ctx, &iam.ListRoleTagsInput{RoleName: aws.String(name)})
	if err != nil {
		return nil
	}
	return iamTags(out.Tags)
}

func (c *awsInventoryCollector) iamInstanceProfileTags(ctx context.Context, client *iam.Client, name string) map[string]string {
	out, err := client.ListInstanceProfileTags(ctx, &iam.ListInstanceProfileTagsInput{InstanceProfileName: aws.String(name)})
	if err != nil {
		return nil
	}
	return iamTags(out.Tags)
}

func (c *awsInventoryCollector) acmTags(ctx context.Context, client *acm.Client, arn string) map[string]string {
	out, err := client.ListTagsForCertificate(ctx, &acm.ListTagsForCertificateInput{CertificateArn: aws.String(arn)})
	if err != nil {
		return nil
	}
	tags := map[string]string{}
	for _, tag := range out.Tags {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return tags
}

func ec2Tags(tags []ec2Types.Tag) map[string]string {
	out := map[string]string{}
	for _, tag := range tags {
		out[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return out
}

func elbTags(tags []elbTypes.Tag) map[string]string {
	out := map[string]string{}
	for _, tag := range tags {
		out[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return out
}

func iamTags(tags []iamTypes.Tag) map[string]string {
	out := map[string]string{}
	for _, tag := range tags {
		out[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return out
}
