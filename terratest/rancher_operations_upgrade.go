package test

import (
	"context"
	"encoding/json"
	"fmt"
	version "github.com/hashicorp/go-version"
	"net/url"
	"strings"
	"time"
)

func (p *localControlPanel) planRancherUpgrade(ctx context.Context, cluster clusterView, in rancherOperationRequest) (*rancherUpgradePlan, error) {
	return p.planRancherUpgradeWithRuntime(ctx, cluster, in, rancherUpgradeRuntime{
		installed: inspectInstalledRancher, inspectImage: inspectRancherImageReference, command: operationCommand,
	})
}

func (p *localControlPanel) planRancherUpgradeWithRuntime(ctx context.Context, cluster clusterView, in rancherOperationRequest, runtime rancherUpgradeRuntime) (*rancherUpgradePlan, error) {
	before, err := runtime.installed(ctx, cluster.KubeconfigPath)
	if err != nil {
		return nil, err
	}
	repo := helmLabRepositories[in.Distribution+"/"+in.Channel]
	if repo == "" {
		return nil, fmt.Errorf("choose an available chart distribution and channel")
	}
	if in.Version == "" {
		return nil, fmt.Errorf("select an exact published chart version")
	}
	catalog, err := p.helmCatalog.load(ctx, repo, in.Channel, in.Version, false)
	if err != nil {
		return nil, err
	}
	plan := &rancherUpgradePlan{ID: operationID(), ClusterID: cluster.ID, Created: time.Now().UTC(), Before: before, Repository: repo, Chart: catalog.Selected, Experimental: in.Channel != "ga" || in.Image != "" || helmLabVersionChannel(before.Version) != "ga", kubeconfig: cluster.KubeconfigPath, Checks: []string{"Installed Helm release is deployed", "Rancher deployment is fully ready", "Running server version read from Rancher", "Published chart checksum verified"}, Warnings: []string{"Take and verify a Rancher backup before upgrading. Helm rollback does not reverse Rancher data migrations.", "Review release notes and the Rancher support matrix for management and downstream Kubernetes versions."}}
	target := catalog.Selected.AppVersion
	if target == "" {
		target = catalog.Selected.Version
	}
	if err = validateUpgradeStep(before.Version, target, plan.Experimental); in.Image == "" && err != nil {
		return nil, err
	}
	if !plan.Experimental {
		stable, loadErr := p.helmCatalog.load(ctx, helmLabRepositories[in.Distribution+"/ga"], "ga", "", false)
		if loadErr != nil {
			return nil, loadErr
		}
		a, _ := version.NewVersion(before.Version)
		b, _ := version.NewVersion(target)
		if a.Segments()[1] != b.Segments()[1] {
			for _, item := range stable.Versions {
				v, e := version.NewVersion(item.AppVersion)
				if e != nil {
					continue
				}
				if v.Segments()[0] != a.Segments()[0] {
					continue
				}
				if v.Segments()[1] == a.Segments()[1] && v.GreaterThan(a) {
					return nil, fmt.Errorf("upgrade to the latest current-minor patch (%s) before changing minor versions", item.AppVersion)
				}
				if v.Segments()[1] == b.Segments()[1] && v.GreaterThan(b) {
					return nil, fmt.Errorf("choose the latest next-minor patch (%s)", item.AppVersion)
				}
			}
		}
	}
	plan.Image, plan.ImageTag, plan.imageFields, err = rancherUpgradeImageSettings(catalog.Chart, target, in.Image)
	if err != nil {
		return nil, err
	}
	if in.Image != "" {
		plan.Warnings = append(plan.Warnings, "Custom/head images are experimental. The selected chart must be compatible with this image; mutable tags are rechecked immediately before execution.")
	}
	provenance, found, err := runtime.inspectImage(ctx, plan.Image+":"+plan.ImageTag)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("target Rancher image does not exist")
	}
	plan.Digest = provenance.Digest
	plan.ImageRevision, plan.ImageOSSRevision, plan.ImageCanonicalReference = provenance.Revision, provenance.OSSRevision, provenance.CanonicalReference
	plan.TargetVersion = target
	if in.Image != "" {
		plan.TargetVersion, plan.TargetVersionSource, err = resolveUpgradeImageVersion(provenance, plan.ImageTag)
		plan.TargetVersionLabel = provenance.BuildVersion
		if err != nil {
			return nil, err
		}
		if err = validateUpgradeStep(before.Version, plan.TargetVersion, true); err != nil {
			return nil, fmt.Errorf("custom image upgrade path cannot be verified: %w", err)
		}
	}
	if minorHeadVersion.MatchString(plan.TargetVersion) || minorHeadVersion.MatchString(before.Version) {
		plan.Warnings = append(plan.Warnings, "This commit-based head declares a release line but no patch number. Patch-level upgrade/downgrade ordering cannot be verified; review the build commit and release history before confirming this experimental transition.")
	}
	plan.AgentImage = strings.TrimSuffix(plan.Image, "/rancher") + "/rancher-agent:" + plan.ImageTag
	agent, agentFound, agentErr := runtime.inspectImage(ctx, plan.AgentImage)
	if agentErr != nil || !agentFound || agent.Digest == "" {
		return nil, fmt.Errorf("matching Rancher agent image cannot be verified: %s", plan.AgentImage)
	}
	plan.AgentDigest = agent.Digest
	if plan.Digest == "" {
		return nil, fmt.Errorf("target image did not resolve to an immutable digest")
	}
	plan.Checks = append(plan.Checks, "Resolved target follows the current-or-next-minor version rule", "Target server and matching agent images exist; both digests recorded")
	if plan.Experimental {
		plan.Warnings = append(plan.Warnings, "Experimental upgrade: RC, alpha and head transitions are not asserted to be supported upgrade paths.")
	}
	base, _ := url.Parse(repo + "/")
	ref, _ := url.Parse(catalog.Selected.URLs[0])
	plan.archive, err = p.helmCatalog.fetch(ctx, base.ResolveReference(ref).String(), 8<<20, false)
	if err != nil {
		return nil, err
	}
	if err = preflightRancherUpgrade(ctx, plan, runtime.command); err != nil {
		return nil, err
	}
	if plan.SystemDefaultRegistry != nil {
		source := *plan.SystemDefaultRegistry
		if source == "" {
			source = "Docker Hub (no registry prefix)"
		}
		plan.Checks = append(plan.Checks, "System images will use "+source)
		plan.Warnings = append(plan.Warnings, "The system image registry follows the selected official Rancher distribution. Downstream creation separately verifies the running server's machine provisioner image.")
	} else {
		plan.Warnings = append(plan.Warnings, "Custom system registry preserved. Ensure it contains this Rancher build's system images before provisioning downstream clusters.")
	}
	plan.Checks = append(plan.Checks, "Helm server dry-run passed with the current release values and selected images")
	p.rancherOps.mu.Lock()
	defer p.rancherOps.mu.Unlock()
	if p.rancherOps.plans == nil {
		p.rancherOps.plans = map[string]*rancherUpgradePlan{}
	}
	for id, old := range p.rancherOps.plans {
		if time.Since(old.Created) > 15*time.Minute {
			delete(p.rancherOps.plans, id)
		}
	}
	p.rancherOps.plans[plan.ID] = plan
	return plan, nil
}

func (p *localControlPanel) startRancherUpgrade(ctx context.Context, cluster clusterView, in rancherOperationRequest) (any, error) {
	p.rancherOps.mu.Lock()
	plan := p.rancherOps.plans[in.PlanID]
	p.rancherOps.mu.Unlock()
	if plan == nil || plan.ClusterID != cluster.ID || time.Since(plan.Created) > 15*time.Minute {
		return nil, fmt.Errorf("review a fresh upgrade plan first (plans expire after 15 minutes)")
	}
	if !in.BackupConfirmed || (plan.Experimental && !in.ExperimentalConfirmed) {
		return nil, fmt.Errorf("confirm your backup and any experimental upgrade warning")
	}
	id := operationID()
	if err := p.reserveRancherOperation(cluster.ID, id); err != nil {
		return nil, err
	}
	before, err := inspectInstalledRancher(ctx, plan.kubeconfig)
	if err != nil {
		p.releaseRancherOperation(cluster.ID)
		return nil, err
	}
	if before.Revision != plan.Before.Revision || before.Image != plan.Before.Image || before.Version != plan.Before.Version {
		p.releaseRancherOperation(cluster.ID)
		return nil, fmt.Errorf("installed Rancher changed since review; generate a new plan")
	}
	record := &rancherOperationRecord{ID: id, ClusterID: cluster.ID, Kind: "upgrade", Status: "running", Started: time.Now().UTC(), From: before.Version, To: plan.Image + ":" + plan.ImageTag, Plan: plan, Events: []rancherOperationEvent{}}
	if err = p.operationEvent(record, "Upgrade approved; backup acknowledged. Rechecking target image and Helm compatibility."); err != nil {
		p.releaseRancherOperation(cluster.ID)
		return nil, err
	}
	p.rancherOps.mu.Lock()
	delete(p.rancherOps.plans, plan.ID)
	p.rancherOps.mu.Unlock()
	if !p.workers.Start(func(context.Context) { p.finishRancherOperation(record, p.executeRancherUpgrade(record, plan)) }) {
		p.finishRancherOperation(record, fmt.Errorf("Runway shut down before the operation started"))
		return nil, fmt.Errorf("Runway is shutting down")
	}
	return map[string]string{"id": record.ID, "status": "running"}, nil
}

type rancherUpgradeRuntime struct {
	inspectImage rancherImageInspectFunc
	command      func(context.Context, []byte, string, ...string) ([]byte, error)
	installed    func(context.Context, string) (rancherInstalledState, error)
	pods         func(string) ([]podView, error)
}

func (p *localControlPanel) executeRancherUpgrade(record *rancherOperationRecord, plan *rancherUpgradePlan) error {
	return p.runRancherUpgrade(record, plan, rancherUpgradeRuntime{inspectRancherImageReference, operationCommand, inspectInstalledRancher, fetchLocalRancherPods})
}

func (p *localControlPanel) runRancherUpgrade(record *rancherOperationRecord, plan *rancherUpgradePlan, runtime rancherUpgradeRuntime) error {
	ctx, cancel := context.WithTimeout(p.workers.Context(), 35*time.Minute)
	defer cancel()
	provenance, found, err := runtime.inspectImage(ctx, plan.Image+":"+plan.ImageTag)
	if err != nil || !found || provenance.Digest != plan.Digest {
		return fmt.Errorf("target image changed or cannot be verified; review a new plan")
	}
	agent, agentFound, agentErr := runtime.inspectImage(ctx, plan.AgentImage)
	if agentErr != nil || !agentFound || agent.Digest != plan.AgentDigest {
		return fmt.Errorf("target agent image changed or cannot be verified; review a new plan")
	}
	args, cleanup, err := prepareRancherUpgrade(ctx, plan, runtime.command)
	if err != nil {
		return err
	}
	defer cleanup()
	commandArgs := []string{"helm", "upgrade", "rancher", args[4], "--kubeconfig", plan.kubeconfig}
	commandArgs = append(commandArgs, args[5:]...)
	commandArgs = append(commandArgs, "--wait", "--wait-for-jobs")
	quoted := make([]string, len(commandArgs))
	for i, arg := range commandArgs {
		quoted[i] = shellQuote(arg)
	}
	p.recordClusterHistory(record.ClusterID, "helm-upgrade-command", map[string]string{"operationId": record.ID, "command": sanitizeTestPackageHelmCommand(strings.Join(quoted, " ")), "source": "Upgrade invocation; local files and credentials redacted; chart and agent digests are in the operation plan"})
	// Repeat Kubernetes-aware Helm rendering immediately before applying the upgrade.
	if _, err = runtime.command(ctx, nil, "helm", append(append([]string{}, args...), "--dry-run=server")...); err != nil {
		return fmt.Errorf("Helm preflight failed; no upgrade applied: %w", err)
	}
	if err = p.operationEvent(record, "Helm server dry-run passed. Applying chart; watching pods and image IDs every five seconds."); err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() {
		_, err := runtime.command(ctx, nil, "helm", append(args, "--wait", "--wait-for-jobs")...)
		result <- err
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	last := ""
	capture := func() error {
		pods, err := runtime.pods(plan.kubeconfig)
		if err != nil {
			return p.operationEvent(record, "Pod observation temporarily unavailable during rollout")
		}
		for i := range pods {
			pods[i].Age = ""
		}
		raw, _ := json.Marshal(pods)
		if string(raw) != last {
			last = string(raw)
			return p.operationEvent(record, "Pods: "+last)
		}
		return nil
	}
	for {
		select {
		case <-ticker.C:
			if err = capture(); err != nil {
				cancel()
				<-result
				return fmt.Errorf("history storage failed during upgrade: %w", err)
			}
		case err = <-result:
			_ = capture()
			if err != nil {
				return fmt.Errorf("upgrade failed or timed out; resources may have changed. No automatic rollback was attempted: %w", err)
			}
			var after rancherInstalledState
			readyDeadline := time.NewTimer(2 * time.Minute)
			defer readyDeadline.Stop()
			for {
				after, err = runtime.installed(ctx, plan.kubeconfig)
				if err == nil && strings.TrimPrefix(after.Version, "v") == strings.TrimPrefix(plan.TargetVersion, "v") {
					break
				}
				select {
				case <-readyDeadline.C:
					return fmt.Errorf("post-upgrade server readiness/version did not converge within two minutes")
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
					if e := capture(); e != nil {
						return e
					}
				}
			}
			if after.Version != plan.TargetVersion && strings.TrimPrefix(after.Version, "v") != strings.TrimPrefix(plan.TargetVersion, "v") {
				return fmt.Errorf("reported server version %s differs from planned version %s", after.Version, plan.TargetVersion)
			}
			if after.Image != plan.Image+":"+plan.ImageTag {
				return fmt.Errorf("running deployment image differs from the reviewed target")
			}
			if err = verifyUpgradeRegistry(ctx, plan, runtime.command); err != nil {
				return err
			}
			raw, _ := json.Marshal(after)
			if err = p.operationEvent(record, "Verified installed state: "+string(raw)); err != nil {
				return err
			}
			record.To = after.Version + " (" + after.Image + ")"
			return nil
		case <-ctx.Done():
			<-result
			return fmt.Errorf("upgrade deadline exceeded; inspect live state before retrying")
		}
	}
}
