package test

import (
	"github.com/brudnak/ha-rancher-rke2/internal/prbuild"

	"fmt"
)

const (
	prBuildTestHeadSHA  = "1111111111111111111111111111111111111111"
	prBuildTestMergeSHA = "2222222222222222222222222222222222222222"
	prBuildTestImageSHA = "3333333333333333333333333333333333333333"
)

func testPRBuildPull(number int) prbuild.GitHubPull {
	var pull prbuild.GitHubPull
	pull.Number = number
	pull.HTMLURL = fmt.Sprintf("https://github.com/rancher/rancher/pull/%d", number)
	pull.Title = "Verify the fix"
	pull.State = "open"
	pull.Head.SHA = prBuildTestHeadSHA
	pull.Head.Ref = "fix-branch"
	pull.Head.Repo.FullName = "rancher/rancher"
	pull.Base.SHA = prBuildTestMergeSHA
	pull.Base.Ref = "release-v2.14"
	pull.Base.Repo.FullName = "rancher/rancher"
	return pull
}
