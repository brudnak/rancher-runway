# Runway workbench theme

Home, Setup, Helm Lab, Steve Lab, K3D Lab, Cache Lab, Image Lookup, PR Image Check,
Issue Radar, AWS Inventory, and Destroy (including Costs & local data) use the shared `--runway-*` foundation in
`terratest/ui/static/control_panel.tailwind.css`. Their existing component tokens
alias this foundation. Change shared values there rather than creating another
slightly different dark palette. Rebuild with `npm run build:panel-ui`.

| Token | Purpose |
| --- | --- |
| `--runway-card` | Main workbench; warm off-white / dark blue `#191e26` |
| `--runway-raised` | Raised controls and table headers |
| `--runway-soft` | Recessed sidebars and supporting surfaces |
| `--runway-input`, `--runway-code` | Editable fields and code surfaces |
| `--runway-ink`, `--runway-muted` | Primary and secondary text |
| `--runway-border`, `--runway-control` | Surface dividers and stronger input borders |
| `--runway-accent`, `--runway-accent-soft` | Primary actions, selection, and focus |
| `--runway-gold`, `--runway-error` | Attention and destructive/error states |
| `--runway-shadow` | Restrained depth at the workbench boundary |

Keep each tool's layout fitted to its task. Share clear focus rings, consistent
control sizing, restrained radii, compact uppercase section labels, and a
progressive disclosure pattern for advanced settings. Never rely on color alone
for status or before/after meaning. Honor reduced motion and test a narrow
viewport as well as the desktop layout.

Setup shares the compiled stylesheet in embedded and standalone modes. Its run
brief contains topology and version metadata only; keep credentials out of it.
Keep approval and lifecycle checks independent of presentation changes.
