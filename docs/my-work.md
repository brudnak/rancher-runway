# My Work

My Work is a personal issue workspace for following a milestone from intake through test preparation and validation. The Home screen introduces that journey and shows a small summary of the latest saved snapshot.

Choose a repository and GitHub milestone, then select **Assigned to me**, **Unassigned**, or **Entire milestone**. For assigned work, Runway can use the authenticated GitHub account or a username you enter. Labels are optional. Pulling reads the milestone and its issues from GitHub; it does not edit GitHub.

Runway creates or reuses a local bucket for the repository and milestone. Open issues without an existing package receive a starter package linked to the issue, except those labeled **QA/None**. Existing plans and sessions are preserved. Existing packages already filed in another bucket keep their placement; unfiled packages matching the milestone are filed with it when the bucket is first created. Issues that leave the selected scope remain visible as departed issues, and their packages stay in the library.

The issue list links directly to its GitHub issue and opens the linked package or offers to create one. Coverage means that an open issue has at least one saved case. The stages distinguish planning, reproduction, and validation. **Validated** is Runway session evidence; **Closed on GitHub** is the issue state. Neither state implies the other.

The remaining-work chart records up to 60 observations for the same repository, milestone, owner scope, username, and labels. Changing any of those starts a separate trend. Snapshots and package organization are stored locally; **Refresh issues** reads current GitHub ownership, milestone membership, labels, and state without creating or deleting packages. Opening My Work, returning to the app, and completing a readiness scan also refresh the scope (routine focus refreshes are limited to once per minute). **Refresh & prepare** additionally creates missing starter packages. Issues labeled **QA/None** remain available in the “QA not required” filter and are excluded from remaining QA work and missing-preparation counts. The milestone form is collapsible and the searchable GitHub chooser can be hidden.

Use [Test Packages](test-packages.md) to edit plans, manage bucket placement, and export individual packages or the full library.

The daily briefing above the milestone intake reports observed build availability, **QA template found on GitHub**, and missing locally saved test cases. Enable automatic scans in Settings, or use **Scan now**. Reports retain their scope and time across restarts; their scope refresh updates the intake snapshot while preserving package placement. See [Issue readiness](issue-readiness.md) for evidence rules and scan limits.
