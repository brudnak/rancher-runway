package test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
)

func TestResolvePatchHeadStagingBundleUsesIndependentStageBudgets(t *testing.T) {
	previousLister := listPatchHeadStagingImageTags
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() {
		listPatchHeadStagingImageTags = previousLister
		inspectPreferredRancherImage = previousInspector
	})
	sha := strings.Repeat("a", 40)
	var listContext context.Context
	var listDeadline time.Time
	listPatchHeadStagingImageTags = func(ctx context.Context, registry, repository string) ([]string, error) {
		listContext = ctx
		var ok bool
		listDeadline, ok = ctx.Deadline()
		if !ok {
			return nil, errors.New("tag listing has no deadline")
		}
		return []string{"v2.16.0-" + sha + "-head"}, nil
	}
	inspectPreferredRancherImage = func(ctx context.Context, reference string) (imagelookup.Provenance, bool, error) {
		if listContext.Err() != context.Canceled {
			return imagelookup.Provenance{}, false, errors.New("tag-list context was not canceled before inspection")
		}
		if err := ctx.Err(); err != nil {
			return imagelookup.Provenance{}, false, fmt.Errorf("tag-list cancellation reached image inspection: %w", err)
		}
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.After(listDeadline) {
			return imagelookup.Provenance{}, false, errors.New("inspection did not receive a fresh scan deadline")
		}
		return patchHeadTimeoutTestProvenance(reference, sha), true, nil
	}

	version, images, err := resolvePatchHeadStagingBundleWithTimeouts(context.Background(), "2.16.0-head", patchHeadStagingTimeouts{
		tagList: time.Second,
		scan:    time.Second,
		image:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("resolve with independent stage deadlines: %v", err)
	}
	if version != "2.16.0-"+sha+"-head" || images == nil {
		t.Fatalf("unexpected resolution: version=%q images=%#v", version, images)
	}
}

func TestResolvePatchHeadStagingBundleImageTimeoutRejectsPartialSuccess(t *testing.T) {
	for _, blockedRole := range []string{"server", "agent"} {
		t.Run(blockedRole, func(t *testing.T) {
			previousInspector := inspectPreferredRancherImage
			t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })
			completeSHA, blockedSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
			stubPatchHeadTagList(t, []string{"v2.16.0-" + completeSHA + "-head", "v2.16.0-" + blockedSHA + "-head"})
			completePairInspected := make(chan struct{})
			inspectPreferredRancherImage = func(ctx context.Context, reference string) (imagelookup.Provenance, bool, error) {
				role := "server"
				if strings.Contains(reference, "rancher-agent:") {
					role = "agent"
				}
				if strings.Contains(reference, blockedSHA) {
					if role != blockedRole {
						return patchHeadTimeoutTestProvenance(reference, blockedSHA), true, nil
					}
					select {
					case <-completePairInspected:
					case <-ctx.Done():
						return imagelookup.Provenance{}, false, ctx.Err()
					}
					<-ctx.Done()
					return imagelookup.Provenance{}, false, ctx.Err()
				}
				if role == "agent" {
					close(completePairInspected)
				}
				return patchHeadTimeoutTestProvenance(reference, completeSHA), true, nil
			}

			version, images, err := resolvePatchHeadStagingBundleWithTimeouts(context.Background(), "2.16.0-head", patchHeadStagingTimeouts{
				tagList: time.Second,
				scan:    5 * time.Second,
				image:   100 * time.Millisecond,
			})
			if err == nil || !strings.Contains(err.Error(), "could not safely resolve") || !strings.Contains(err.Error(), "inspect "+blockedRole+" image") || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
				t.Fatalf("expected individual image deadline to fail the whole resolution, got %v", err)
			}
			if version != "" || images != nil {
				t.Fatalf("returned a partial result after an image timeout: version=%q images=%#v", version, images)
			}
			select {
			case <-completePairInspected:
			default:
				t.Fatal("complete candidate was not inspected")
			}
		})
	}
}

func TestResolvePatchHeadStagingBundleTagListTimeoutSkipsInspection(t *testing.T) {
	previousLister := listPatchHeadStagingImageTags
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() {
		listPatchHeadStagingImageTags = previousLister
		inspectPreferredRancherImage = previousInspector
	})
	listPatchHeadStagingImageTags = func(ctx context.Context, registry, repository string) ([]string, error) {
		<-ctx.Done()
		return []string{"v2.16.0-" + strings.Repeat("a", 40) + "-head"}, ctx.Err()
	}
	inspected := make(chan struct{}, 1)
	inspectPreferredRancherImage = func(ctx context.Context, reference string) (imagelookup.Provenance, bool, error) {
		select {
		case inspected <- struct{}{}:
		default:
		}
		return imagelookup.Provenance{}, false, errors.New("unexpected image inspection after tag-list timeout")
	}

	version, images, err := resolvePatchHeadStagingBundleWithTimeouts(context.Background(), "2.16.0-head", patchHeadStagingTimeouts{
		tagList: 100 * time.Millisecond,
		scan:    5 * time.Second,
		image:   time.Second,
	})
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "list immutable Rancher head images") {
		t.Fatalf("expected wrapped tag-list deadline, got %v", err)
	}
	if version != "" || images != nil {
		t.Fatalf("returned a result after tag-list timeout: version=%q images=%#v", version, images)
	}
	select {
	case <-inspected:
		t.Fatal("image inspection started despite tag-list timeout")
	default:
	}
}

func TestResolvePatchHeadStagingBundleScanTimeoutRejectsPartialSuccess(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })
	completeSHA, blockedSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	stubPatchHeadTagList(t, []string{"v2.16.0-" + completeSHA + "-head", "v2.16.0-" + blockedSHA + "-head"})
	inspectPreferredRancherImage = func(ctx context.Context, reference string) (imagelookup.Provenance, bool, error) {
		if strings.Contains(reference, blockedSHA) {
			<-ctx.Done()
			return imagelookup.Provenance{}, false, ctx.Err()
		}
		return patchHeadTimeoutTestProvenance(reference, completeSHA), true, nil
	}

	version, images, err := resolvePatchHeadStagingBundleWithTimeouts(context.Background(), "2.16.0-head", patchHeadStagingTimeouts{
		tagList: time.Second,
		scan:    200 * time.Millisecond,
		image:   5 * time.Second,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected wrapped scan deadline error, got %v", err)
	}
	if !strings.Contains(err.Error(), "inspected 1 of 2 candidate pairs") || !strings.Contains(err.Error(), "2.16.0-") {
		t.Fatalf("scan deadline omitted progress or immutable-version guidance: %v", err)
	}
	if version != "" || images != nil {
		t.Fatalf("returned a partial result after the scan deadline: version=%q images=%#v", version, images)
	}
}

func TestResolvePatchHeadStagingBundleParentCancellationRejectsPartialSuccess(t *testing.T) {
	previousInspector := inspectPreferredRancherImage
	t.Cleanup(func() { inspectPreferredRancherImage = previousInspector })
	completeSHA, blockedSHA := strings.Repeat("a", 40), strings.Repeat("b", 40)
	stubPatchHeadTagList(t, []string{"v2.16.0-" + completeSHA + "-head", "v2.16.0-" + blockedSHA + "-head"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completePairInspected := make(chan struct{})
	inspectPreferredRancherImage = func(imageCtx context.Context, reference string) (imagelookup.Provenance, bool, error) {
		if strings.Contains(reference, blockedSHA) {
			select {
			case <-completePairInspected:
				cancel()
			case <-imageCtx.Done():
			}
			<-imageCtx.Done()
			return imagelookup.Provenance{}, false, imageCtx.Err()
		}
		if strings.Contains(reference, "rancher-agent:") {
			close(completePairInspected)
		}
		return patchHeadTimeoutTestProvenance(reference, completeSHA), true, nil
	}

	version, images, err := resolvePatchHeadStagingBundleWithTimeouts(ctx, "2.16.0-head", patchHeadStagingTimeouts{
		tagList: time.Second,
		scan:    5 * time.Second,
		image:   time.Second,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected parent cancellation to be preserved, got %v", err)
	}
	if version != "" || images != nil {
		t.Fatalf("returned a partial result after parent cancellation: version=%q images=%#v", version, images)
	}
}

func patchHeadTimeoutTestProvenance(reference, sha string) imagelookup.Provenance {
	return imagelookup.Provenance{
		Reference:          reference,
		Digest:             "sha256:" + strings.Repeat(sha[:1], 64),
		CreatedAt:          time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC),
		SourceURL:          "https://github.com/rancher/rancher-prime",
		OSSRevision:        sha,
		CanonicalReference: strings.TrimPrefix(reference, "stgregistry.suse.com/"),
	}
}
