package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamTypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"golang.org/x/sync/errgroup"
)

// These instructions are constructed only by a fresh AWS inspection. A role
// review explicitly includes its owned profiles and policy detachments, just as
// a load-balancer review includes its listeners. Managed policies are never deleted.
type awsIAMCleanupDetails struct {
	RoleName string
	Policies []string
	Profiles []awsIAMProfileRemoval
}

type awsIAMProfileRemoval struct {
	Name  string
	Roles []string
}

func iamCleanupName(id, kind string) (string, error) {
	parsed, err := arn.Parse(id)
	if err != nil || parsed.Service != "iam" || parsed.Region != "" || parsed.AccountID == "" || !strings.HasPrefix(parsed.Resource, kind+"/") {
		return "", fmt.Errorf("Invalid IAM %s ARN.", kind)
	}
	name := parsed.Resource[strings.LastIndex(parsed.Resource, "/")+1:]
	if name == "" {
		return "", fmt.Errorf("IAM resource name is missing.")
	}
	return name, nil
}

func iamAttachmentARNs(id string) (string, string, error) {
	role, policy, ok := strings.Cut(id, ":arn:")
	if !ok {
		return "", "", fmt.Errorf("Invalid IAM policy attachment identity.")
	}
	policy = "arn:" + policy
	if _, err := iamCleanupName(role, "role"); err != nil {
		return "", "", err
	}
	if _, err := iamCleanupName(policy, "policy"); err != nil {
		return "", "", err
	}
	return role, policy, nil
}

// Arbitrary IAM trust relationships cannot be checked with EC2 APIs. Restrict
// this cleanup path to the EC2-only roles Runway creates, including China regions.
func iamEC2OnlyTrust(raw string) bool {
	if decoded, err := url.QueryUnescape(raw); err == nil {
		raw = decoded
	}
	var policy struct {
		Statement []struct {
			Effect       string
			Principal    map[string]json.RawMessage
			NotPrincipal json.RawMessage
			Action       json.RawMessage
			NotAction    json.RawMessage
		}
	}
	if json.Unmarshal([]byte(raw), &policy) != nil || len(policy.Statement) == 0 {
		return false
	}
	for _, statement := range policy.Statement {
		if statement.Effect != "Allow" || len(statement.Principal) != 1 || len(statement.NotPrincipal) != 0 || len(statement.NotAction) != 0 {
			return false
		}
		services := iamStringList(statement.Principal["Service"])
		actions := iamStringList(statement.Action)
		if len(services) == 0 || len(actions) != 1 || actions[0] != "sts:AssumeRole" {
			return false
		}
		for _, service := range services {
			if service != "ec2.amazonaws.com" && service != "ec2.amazonaws.com.cn" {
				return false
			}
		}
	}
	return true
}

func iamStringList(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

func sameIAMRunOwnership(left, right map[string]string) bool {
	for _, key := range []string{"Owner", "ManagedBy", "HA_Rancher_RKE2_Run_ID", "NamePrefix"} {
		if left[key] != right[key] {
			return false
		}
	}
	return left["Owner"] != "" && awsManagedByTagValues[left["ManagedBy"]] && left["HA_Rancher_RKE2_Run_ID"] != ""
}

func (c *awsSDKCleanupClient) cleanupIAMRole(ctx context.Context, id string) (*iamTypes.Role, error) {
	name, err := iamCleanupName(id, "role")
	if err != nil {
		return nil, err
	}
	out, err := c.iam.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(name)})
	if err != nil {
		return nil, awsCleanupAPIError(err)
	}
	if out.Role == nil || aws.ToString(out.Role.Arn) != id || aws.ToString(out.Role.RoleName) != name || aws.ToString(out.Role.RoleId) == "" {
		return nil, fmt.Errorf("Cannot verify IAM role identity.")
	}
	if !iamEC2OnlyTrust(aws.ToString(out.Role.AssumeRolePolicyDocument)) {
		return nil, fmt.Errorf("Role %s is not trusted exclusively by EC2. Its other consumers cannot be verified by this cleanup.", name)
	}
	return out.Role, nil
}

func (c *awsSDKCleanupClient) cleanupIAMProfile(ctx context.Context, id string) (*iamTypes.InstanceProfile, error) {
	name, err := iamCleanupName(id, "instance-profile")
	if err != nil {
		return nil, err
	}
	out, err := c.iam.GetInstanceProfile(ctx, &iam.GetInstanceProfileInput{InstanceProfileName: aws.String(name)})
	if err != nil {
		return nil, awsCleanupAPIError(err)
	}
	if out.InstanceProfile == nil || aws.ToString(out.InstanceProfile.Arn) != id || aws.ToString(out.InstanceProfile.InstanceProfileName) != name || aws.ToString(out.InstanceProfile.InstanceProfileId) == "" {
		return nil, fmt.Errorf("Cannot verify IAM instance profile identity.")
	}
	return out.InstanceProfile, nil
}

func (c *awsSDKCleanupClient) inspectIAMCleanup(ctx context.Context, resource awsResourceView) (awsCleanupInspection, error) {
	inspection := awsCleanupInspection{Effects: []string{}, Identities: map[string]string{}, IAM: &awsIAMCleanupDetails{}}
	if c.iam == nil {
		return inspection, fmt.Errorf("IAM cleanup client is unavailable.")
	}
	item := awsResourceView{Type: resource.Type, ID: resource.ID, Region: "global"}
	var profileARNs []string
	if resource.Type == "IAM instance profile" {
		profile, err := c.cleanupIAMProfile(ctx, resource.ID)
		if err != nil {
			return inspection, err
		}
		item.Name, item.Tags = aws.ToString(profile.InstanceProfileName), iamTags(profile.Tags)
		if err := c.inspectIAMProfileRemoval(ctx, profile, item.Tags, &inspection); err != nil {
			return inspection, err
		}
		profileARNs = append(profileARNs, resource.ID)
	} else {
		roleARN, selectedPolicy := resource.ID, ""
		if resource.Type == "IAM policy attachment" {
			var err error
			roleARN, selectedPolicy, err = iamAttachmentARNs(resource.ID)
			if err != nil {
				return inspection, err
			}
		}
		role, err := c.cleanupIAMRole(ctx, roleARN)
		if err != nil {
			return inspection, err
		}
		item.Name, item.Tags = aws.ToString(role.RoleName), iamTags(role.Tags)
		inspection.Identities[roleARN] = aws.ToString(role.RoleId)
		inspection.IAM.RoleName = item.Name
		profiles := iam.NewListInstanceProfilesForRolePaginator(c.iam, &iam.ListInstanceProfilesForRoleInput{RoleName: role.RoleName})
		for profiles.HasMorePages() {
			page, err := profiles.NextPage(ctx)
			if err != nil {
				return inspection, fmt.Errorf("Cannot verify instance profiles for role %s: %w", item.Name, err)
			}
			for _, listed := range page.InstanceProfiles {
				profileARN := aws.ToString(listed.Arn)
				profileARNs = append(profileARNs, profileARN)
				profile, err := c.cleanupIAMProfile(ctx, profileARN)
				if err != nil {
					return inspection, fmt.Errorf("Cannot verify associated instance profile: %v", err)
				}
				if !sameIAMRunOwnership(item.Tags, iamTags(profile.Tags)) {
					return inspection, fmt.Errorf("Instance profile %s has different or missing ownership tags.", aws.ToString(profile.InstanceProfileName))
				}
				inspection.Identities[profileARN] = aws.ToString(profile.InstanceProfileId)
				if resource.Type == "IAM role" {
					if err := c.inspectIAMProfileRemoval(ctx, profile, item.Tags, &inspection); err != nil {
						return inspection, err
					}
				}
			}
		}
		policies := iam.NewListAttachedRolePoliciesPaginator(c.iam, &iam.ListAttachedRolePoliciesInput{RoleName: role.RoleName})
		foundPolicy := false
		for policies.HasMorePages() {
			page, err := policies.NextPage(ctx)
			if err != nil {
				return inspection, fmt.Errorf("Cannot verify policies for role %s: %w", item.Name, err)
			}
			for _, policy := range page.AttachedPolicies {
				id := aws.ToString(policy.PolicyArn)
				if resource.Type == "IAM role" || id == selectedPolicy {
					inspection.IAM.Policies = append(inspection.IAM.Policies, id)
					inspection.Effects = append(inspection.Effects, "Detach "+id+" from role "+aws.ToString(role.RoleName)+"; the managed policy itself is retained")
				}
				if id == selectedPolicy {
					foundPolicy = true
					item.Name = aws.ToString(policy.PolicyName)
				}
			}
		}
		if resource.Type == "IAM policy attachment" && !foundPolicy {
			return inspection, errAWSResourceGone
		}
		if resource.Type == "IAM role" {
			inline, err := c.iam.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{RoleName: role.RoleName})
			if err != nil {
				return inspection, fmt.Errorf("Cannot verify inline policies: %w", err)
			}
			if len(inline.PolicyNames) > 0 || inline.IsTruncated {
				return inspection, fmt.Errorf("Role %s has inline policies. Review them in IAM before deleting this role.", item.Name)
			}
			inspection.Effects = append(inspection.Effects, "Delete IAM role "+roleARN)
		}
	}
	if len(profileARNs) > 0 {
		regions, err := c.verifyUnusedIAMProfiles(ctx, profileARNs)
		if err != nil {
			return inspection, err
		}
		inspection.Checks = append(inspection.Checks, fmt.Sprintf("Checked all %d enabled AWS regions: no pending, running, stopping, stopped, or shutting-down EC2 instances use the selected profiles.", regions))
	} else {
		inspection.Checks = append(inspection.Checks, "The role trusts only EC2 and has no instance profiles.")
	}
	inspection.Checks = append(inspection.Checks, "Ownership and usage are checked again before deletion. Saved launch configurations may still reference a deleted profile.")
	item.Owner, item.RunID = item.Tags["Owner"], safeRunPathSegment(item.Tags["HA_Rancher_RKE2_Run_ID"])
	inspection.Resource = item
	return inspection, nil
}

func (c *awsSDKCleanupClient) inspectIAMProfileRemoval(ctx context.Context, profile *iamTypes.InstanceProfile, ownerTags map[string]string, inspection *awsCleanupInspection) error {
	if !sameIAMRunOwnership(ownerTags, iamTags(profile.Tags)) {
		return fmt.Errorf("Instance profile %s has different or missing ownership tags; it will not be removed with this role.", aws.ToString(profile.InstanceProfileName))
	}
	profileARN, name := aws.ToString(profile.Arn), aws.ToString(profile.InstanceProfileName)
	inspection.Identities[profileARN] = aws.ToString(profile.InstanceProfileId)
	removal := awsIAMProfileRemoval{Name: name}
	for _, member := range profile.Roles {
		role, err := c.cleanupIAMRole(ctx, aws.ToString(member.Arn))
		if err != nil {
			return fmt.Errorf("Cannot verify profile's role: %v", err)
		}
		if !sameIAMRunOwnership(ownerTags, iamTags(role.Tags)) {
			return fmt.Errorf("Instance profile %s contains a role with different or missing ownership tags.", name)
		}
		inspection.Identities[aws.ToString(role.Arn)] = aws.ToString(role.RoleId)
		roleName := aws.ToString(role.RoleName)
		removal.Roles = append(removal.Roles, roleName)
		inspection.Effects = append(inspection.Effects, "Remove role "+roleName+" from unused instance profile "+name)
	}
	inspection.IAM.Profiles = append(inspection.IAM.Profiles, removal)
	inspection.Effects = append(inspection.Effects, "Delete unused instance profile "+profileARN)
	return nil
}

func (c *awsSDKCleanupClient) verifyUnusedIAMProfiles(ctx context.Context, profileARNs []string) (int, error) {
	if c.ec2 == nil || c.ec2ForRegion == nil {
		return 0, fmt.Errorf("Cannot verify EC2 usage across AWS regions.")
	}
	for _, id := range profileARNs {
		if _, err := iamCleanupName(id, "instance-profile"); err != nil {
			return 0, err
		}
	}
	regions, err := c.ec2.DescribeRegions(ctx, &ec2.DescribeRegionsInput{})
	if err != nil || len(regions.Regions) == 0 {
		return 0, fmt.Errorf("Cannot verify enabled AWS regions; IAM cleanup remains blocked: %v", err)
	}
	group, scanCtx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for _, region := range regions.Regions {
		if aws.ToString(region.RegionName) == "" {
			return 0, fmt.Errorf("AWS returned an unnamed region; IAM cleanup remains blocked.")
		}
	}
	for _, region := range regions.Regions {
		name := aws.ToString(region.RegionName)
		group.Go(func() error {
			client := c.ec2ForRegion(name)
			pages := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{Filters: []ec2Types.Filter{{Name: aws.String("iam-instance-profile.arn"), Values: profileARNs}}})
			for pages.HasMorePages() {
				page, err := pages.NextPage(scanCtx)
				if err != nil {
					return fmt.Errorf("Cannot verify EC2 usage in %s; IAM cleanup remains blocked: %w", name, err)
				}
				for _, reservation := range page.Reservations {
					for _, instance := range reservation.Instances {
						if instance.State == nil || instance.State.Name != ec2Types.InstanceStateNameTerminated {
							return fmt.Errorf("Instance %s in %s still uses this IAM profile. Clean up the instance first.", aws.ToString(instance.InstanceId), name)
						}
					}
				}
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return 0, err
	}
	return len(regions.Regions), nil
}

func (c *awsSDKCleanupClient) deleteIAMCleanup(ctx context.Context, inspection awsCleanupInspection) error {
	plan := inspection.IAM
	if c.iam == nil || plan == nil || len(inspection.Identities) == 0 {
		return fmt.Errorf("IAM deletion requires a fresh, verified cleanup review.")
	}
	for _, profile := range plan.Profiles {
		for _, role := range profile.Roles {
			if _, err := c.iam.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{InstanceProfileName: aws.String(profile.Name), RoleName: aws.String(role)}); err != nil {
				return fmt.Errorf("Could not remove role from profile %s; refresh before retrying: %w", profile.Name, err)
			}
		}
		if _, err := c.iam.DeleteInstanceProfile(ctx, &iam.DeleteInstanceProfileInput{InstanceProfileName: aws.String(profile.Name)}); err != nil {
			return fmt.Errorf("Could not delete instance profile %s; refresh before retrying: %w", profile.Name, err)
		}
	}
	for _, policy := range plan.Policies {
		if _, err := c.iam.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{RoleName: aws.String(plan.RoleName), PolicyArn: aws.String(policy)}); err != nil {
			return fmt.Errorf("Could not detach role policy; refresh before retrying: %w", err)
		}
	}
	if inspection.Resource.Type == "IAM role" {
		_, err := c.iam.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: aws.String(plan.RoleName)})
		return awsCleanupAPIError(err)
	}
	return nil
}
