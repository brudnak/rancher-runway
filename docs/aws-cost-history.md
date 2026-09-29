# AWS cleanup and local cost history

Home's **Clean up. Keep the history.** section connects two related workflows.

- **AWS Inventory** discovers resources matching run prefixes or Owner tags.
  Search and filter by resource type or cleanup eligibility. Selecting all in
  the table selects only visible eligible resources; selections outside a
  filter remain selected. **Review selected** covers that complete selection,
  while **Review visible candidates** covers only the current filtered view.
  Every review rechecks AWS ownership and dependencies and lists exact effects
  before typed confirmation. A local run record, missing ownership evidence,
  or unverified dependency keeps a resource protected. An incomplete scan is
  explicitly identified and cannot claim to cover the whole account.
- **Destroy → Run cleanup** uses the original recorded Terraform context.
  Runs can be searched and selected individually or as a sequential batch.
  Failed management destroys remain recorded for retry. Recorded Linode
  downstream cleanup can fail independently; review warnings for resources
  requiring manual follow-up.

## Costs & local data

When Runway obtains an AWS cost estimate before management destroy and that
cleanup succeeds, it writes the estimate to the local SQLite ledger at
`automation-output/control-panel/cost-ledger.sqlite` in the active workspace.
No estimate is guaranteed if pricing/resource discovery fails. Inventory orphan
cleanup, live runs, and Linode charges do not contribute to this ledger.

The lifetime, calendar-month, ISO-week, and today totals use every ledger row.
Charts also use the full history, grouped by the local date when cleanup
completed. They are **whole-run estimates recorded on that date, not daily AWS
billing**. Use the last 30 days, 90 days, year, or all history, and optionally a
region. Longer spans group into months or years. The cumulative chart starts at
zero for the selected range. Chart points support arrow keys, Home, and End.

The searchable record list shows the newest 200 records, paginated in groups of
15. Open **Details** for service amounts and any captured missing-service notes.
Records restored from a backup are marked **Imported**.

### Estimate model

The cleanup worker queries the AWS Price List API at cleanup time and models:

| Service | Estimate |
| --- | --- |
| EC2 | Linux shared on-demand rate × age since AWS launch time |
| Attached EBS | Capacity price × GiB × volume age / 730 hours per month |
| Matched RDS/Aurora | Instance-class hourly rate × instance age |
| Matched load balancers | Base hourly rate × load-balancer age |

EC2 instance-hours sum the ages of all matched instances; they are not the wall
clock duration of one run. EBS may fall back to average EC2 age when volume age
is unavailable. Prices are current at cleanup time, not historical prices.
Supported price-region mappings currently cover `us-east-1`, `us-east-2`,
`us-west-1`, and `us-west-2`.

This is a local comparison aid, not an AWS bill. It excludes discounts, credits,
taxes, stopped intervals, data transfer, NAT, public IPv4, additional EBS IOPS
or throughput, database storage/I/O, and load-balancer capacity units. New
records retain notes when an RDS or load-balancer price lookup fails. Older
records may not disclose missing services. AWS describes the catalog in its
[Price List documentation](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/price-changes.html).

### Export and import

**Export** always covers the entire ledger, independent of the chart filters.
Files are saved in Downloads with a unique prefix and owner-only file access.

- `runway-aws-costs.csv`: service estimates, totals, metadata, and notes for
  spreadsheets or other analysis tools. CSV cannot be imported into Runway.
- `runway-aws-costs.json`: a versioned backup preserving complete records and
  metadata. JSON bundles support up to 50,000 records and 64 MiB. Larger histories
  can be exported to CSV within the same file-size limit.

**Import** previews a merge. Identity is the run ID, cleanup timestamp, and
source. Identical records are skipped; conflicting records are left unchanged;
new records are added with an import marker. Existing records are never
replaced. The preview is tied to both the input and the current ledger, so an
intervening change requires a fresh preview. Inserts commit as one transaction.
Import is unavailable during lifecycle operations. No AWS credentials or API
calls are needed to view or transfer local cost history.

### Manage local files

Below the journal, **Manage local files** keeps two actions distinct:

- Run-residue cleanup removes eligible leftover run output and shared Terraform
  working files only after recorded runs are gone. It preserves cost history,
  lab workspaces, and saved cattle-configs.
- Cost-journal reset removes the local ledger and its SQLite sidecars. Export a
  JSON backup first if the history matters. Reset does not delete AWS resources
  or Terraform state.

Both retain the existing lifecycle checks and typed confirmations. Disconnecting
or cleaning up a lab's own workspace remains in that lab.
