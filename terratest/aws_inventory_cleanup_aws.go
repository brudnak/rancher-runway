package test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/smithy-go"
)

type awsSDKCleanupClient struct {
	region string
	ec2    *ec2.Client
	elb    *elasticloadbalancingv2.Client
	acm    *acm.Client
}

func newAWSCleanupClient(ctx context.Context, region string) (awsCleanupClient, error) {
	cfg, err := awsConfig(ctx, region)
	if err != nil {
		return nil, err
	}
	return &awsSDKCleanupClient{region: region, ec2: ec2.NewFromConfig(cfg), elb: elasticloadbalancingv2.NewFromConfig(cfg), acm: acm.NewFromConfig(cfg)}, nil
}
func awsCleanupAPIError(err error) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "InvalidInstanceID.NotFound", "InvalidVolume.NotFound", "LoadBalancerNotFound", "ListenerNotFound", "TargetGroupNotFound", "ResourceNotFoundException":
			return errAWSResourceGone
		}
	}
	return err
}
func (c *awsSDKCleanupClient) tags(ctx context.Context, id string) (map[string]string, error) {
	out, err := c.elb.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{ResourceArns: []string{id}})
	if err != nil {
		return nil, awsCleanupAPIError(err)
	}
	if len(out.TagDescriptions) != 1 {
		return nil, fmt.Errorf("Cannot verify tags for %s.", id)
	}
	return elbTags(out.TagDescriptions[0].Tags), nil
}
func (c *awsSDKCleanupClient) listenerEffects(ctx context.Context, id string) ([]string, error) {
	effects := []string{"Delete listener " + id}
	pages := elasticloadbalancingv2.NewDescribeRulesPaginator(c.elb, &elasticloadbalancingv2.DescribeRulesInput{ListenerArn: aws.String(id)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, awsCleanupAPIError(err)
		}
		for _, rule := range page.Rules {
			effects = append(effects, "Delete listener rule "+aws.ToString(rule.RuleArn))
		}
	}
	return effects, nil
}
func (c *awsSDKCleanupClient) Inspect(ctx context.Context, resource awsResourceView) (awsCleanupInspection, error) {
	item := awsResourceView{Type: resource.Type, ID: resource.ID, Region: c.region}
	inspection := awsCleanupInspection{Effects: []string{}}
	switch item.Type {
	case "EC2 instance":
		out, err := c.ec2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{item.ID}})
		if err != nil {
			return inspection, awsCleanupAPIError(err)
		}
		var instances []ec2Types.Instance
		for _, reservation := range out.Reservations {
			instances = append(instances, reservation.Instances...)
		}
		if len(instances) != 1 {
			return inspection, errAWSResourceGone
		}
		instance := instances[0]
		if aws.ToString(instance.InstanceId) != item.ID {
			return inspection, fmt.Errorf("Unexpected instance identity.")
		}
		if instance.State != nil {
			item.Status = string(instance.State.Name)
		}
		if item.Status == "terminated" {
			return inspection, errAWSResourceGone
		}
		item.Tags = ec2Tags(instance.Tags)
		if item.Tags["aws:autoscaling:groupName"] != "" {
			return inspection, fmt.Errorf("This instance belongs to an Auto Scaling group. Clean up its owning stack instead.")
		}
		item.Name = item.Tags["Name"]
		inspection.Effects = append(inspection.Effects, "Terminate instance "+item.ID+"; instance-store data is permanently lost")
		for _, device := range instance.BlockDeviceMappings {
			if device.Ebs == nil {
				continue
			}
			effect := "Preserve EBS volume "
			if aws.ToBool(device.Ebs.DeleteOnTermination) {
				effect = "Permanently delete EBS volume "
			}
			inspection.Effects = append(inspection.Effects, effect+aws.ToString(device.Ebs.VolumeId))
		}
	case "EBS volume":
		out, err := c.ec2.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{VolumeIds: []string{item.ID}})
		if err != nil {
			return inspection, awsCleanupAPIError(err)
		}
		if len(out.Volumes) != 1 {
			return inspection, errAWSResourceGone
		}
		volume := out.Volumes[0]
		if aws.ToString(volume.VolumeId) != item.ID {
			return inspection, fmt.Errorf("Unexpected volume identity.")
		}
		item.Tags, item.Status = ec2Tags(volume.Tags), string(volume.State)
		item.Name = item.Tags["Name"]
		for _, attachment := range volume.Attachments {
			inspection.Dependencies = append(inspection.Dependencies, awsCleanupRef{Type: "EC2 instance", ID: aws.ToString(attachment.InstanceId)})
		}
		inspection.Effects = append(inspection.Effects, "Permanently delete EBS volume "+item.ID+"; no snapshot will be created")
	case "ALB":
		out, err := c.elb.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{LoadBalancerArns: []string{item.ID}})
		if err != nil {
			return inspection, awsCleanupAPIError(err)
		}
		if len(out.LoadBalancers) != 1 {
			return inspection, errAWSResourceGone
		}
		lb := out.LoadBalancers[0]
		item.Name = aws.ToString(lb.LoadBalancerName)
		if lb.State != nil {
			item.Status = string(lb.State.Code)
		}
		item.Tags, err = c.tags(ctx, item.ID)
		if err != nil {
			return inspection, err
		}
		inspection.Effects = append(inspection.Effects, "Delete load balancer "+item.ID+"; its endpoint stops serving traffic")
		pages := elasticloadbalancingv2.NewDescribeListenersPaginator(c.elb, &elasticloadbalancingv2.DescribeListenersInput{LoadBalancerArn: aws.String(item.ID)})
		for pages.HasMorePages() {
			page, err := pages.NextPage(ctx)
			if err != nil {
				return inspection, awsCleanupAPIError(err)
			}
			for _, listener := range page.Listeners {
				effects, err := c.listenerEffects(ctx, aws.ToString(listener.ListenerArn))
				if err != nil {
					return inspection, err
				}
				inspection.Effects = append(inspection.Effects, effects...)
			}
		}
	case "ALB listener":
		out, err := c.elb.DescribeListeners(ctx, &elasticloadbalancingv2.DescribeListenersInput{ListenerArns: []string{item.ID}})
		if err != nil {
			return inspection, awsCleanupAPIError(err)
		}
		if len(out.Listeners) != 1 {
			return inspection, errAWSResourceGone
		}
		listener := out.Listeners[0]
		parentTags, err := c.tags(ctx, aws.ToString(listener.LoadBalancerArn))
		if err != nil {
			return inspection, err
		}
		tags, err := c.tags(ctx, item.ID)
		if err != nil {
			return inspection, err
		}
		for _, key := range []string{"Owner", "ManagedBy", "HA_Rancher_RKE2_Run_ID", "NamePrefix"} {
			if tags[key] != "" && tags[key] != parentTags[key] {
				return inspection, fmt.Errorf("Listener ownership differs from its load balancer.")
			}
		}
		// Listener permissions derive from its parent, including untagged listeners.
		item.Tags = parentTags
		item.Name = fmt.Sprintf("%s:%d", parentTags["Name"], aws.ToInt32(listener.Port))
		item.Status = string(listener.Protocol)
		inspection.Effects, err = c.listenerEffects(ctx, item.ID)
		if err != nil {
			return inspection, err
		}
	case "Target group":
		out, err := c.elb.DescribeTargetGroups(ctx, &elasticloadbalancingv2.DescribeTargetGroupsInput{TargetGroupArns: []string{item.ID}})
		if err != nil {
			return inspection, awsCleanupAPIError(err)
		}
		if len(out.TargetGroups) != 1 {
			return inspection, errAWSResourceGone
		}
		group := out.TargetGroups[0]
		item.Name, item.Status = aws.ToString(group.TargetGroupName), string(group.TargetType)
		item.Tags, err = c.tags(ctx, item.ID)
		if err != nil {
			return inspection, err
		}
		// Require the owning balancer in the selection. AWS also rejects a target
		// group still referenced by any listener rule; never silently remove rules.
		for _, id := range group.LoadBalancerArns {
			inspection.Dependencies = append(inspection.Dependencies, awsCleanupRef{Type: "ALB", ID: id})
		}
		inspection.Effects = append(inspection.Effects, "Delete target group "+item.ID+"; registered targets themselves are retained")
	case "ACM certificate":
		out, err := c.acm.DescribeCertificate(ctx, &acm.DescribeCertificateInput{CertificateArn: aws.String(item.ID)})
		if err != nil {
			return inspection, awsCleanupAPIError(err)
		}
		if out.Certificate == nil {
			return inspection, errAWSResourceGone
		}
		cert := out.Certificate
		item.Name, item.Status = aws.ToString(cert.DomainName), string(cert.Status)
		tags, err := c.acm.ListTagsForCertificate(ctx, &acm.ListTagsForCertificateInput{CertificateArn: aws.String(item.ID)})
		if err != nil {
			return inspection, awsCleanupAPIError(err)
		}
		item.Tags = map[string]string{}
		for _, tag := range tags.Tags {
			item.Tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
		for _, id := range cert.InUseBy {
			if !strings.Contains(id, ":loadbalancer/") {
				return inspection, fmt.Errorf("Certificate is in use by %s. Remove it from that service first.", id)
			}
			inspection.Dependencies = append(inspection.Dependencies, awsCleanupRef{Type: "ALB", ID: id})
		}
		inspection.Effects = append(inspection.Effects, "Delete ACM certificate "+item.ID)
	default:
		return inspection, fmt.Errorf("This resource type is protected from inventory cleanup.")
	}
	item.Owner = item.Tags["Owner"]
	item.RunID = safeRunPathSegment(item.Tags["HA_Rancher_RKE2_Run_ID"])
	inspection.Resource = item
	return inspection, nil
}
func (c *awsSDKCleanupClient) Delete(ctx context.Context, item awsCleanupInspection) error {
	id := aws.String(item.Resource.ID)
	var err error
	switch item.Resource.Type {
	case "EC2 instance":
		_, err = c.ec2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{InstanceIds: []string{*id}})
		if err == nil {
			if waitErr := ec2.NewInstanceTerminatedWaiter(c.ec2).Wait(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{*id}}, 3*time.Minute); waitErr != nil {
				return fmt.Errorf("Termination requested; AWS has not confirmed completion. Refresh inventory before retrying: %w", waitErr)
			}
		}
	case "EBS volume":
		if item.Resource.Status != "available" || len(item.Dependencies) > 0 {
			return fmt.Errorf("Volume is still attached or unavailable; nothing was detached. Retry after its instance finishes terminating.")
		}
		_, err = c.ec2.DeleteVolume(ctx, &ec2.DeleteVolumeInput{VolumeId: id})
	case "ALB listener":
		_, err = c.elb.DeleteListener(ctx, &elasticloadbalancingv2.DeleteListenerInput{ListenerArn: id})
	case "ALB":
		_, err = c.elb.DeleteLoadBalancer(ctx, &elasticloadbalancingv2.DeleteLoadBalancerInput{LoadBalancerArn: id})
		if err == nil {
			if waitErr := elasticloadbalancingv2.NewLoadBalancersDeletedWaiter(c.elb).Wait(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{LoadBalancerArns: []string{*id}}, 3*time.Minute); waitErr != nil {
				return fmt.Errorf("Load balancer deletion requested; AWS has not confirmed completion. Refresh inventory before retrying: %w", waitErr)
			}
		}
	case "Target group":
		_, err = c.elb.DeleteTargetGroup(ctx, &elasticloadbalancingv2.DeleteTargetGroupInput{TargetGroupArn: id})
	case "ACM certificate":
		_, err = c.acm.DeleteCertificate(ctx, &acm.DeleteCertificateInput{CertificateArn: id})
	default:
		return fmt.Errorf("This resource type is protected from inventory cleanup.")
	}
	return awsCleanupAPIError(err)
}
