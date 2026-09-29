# Rancher Runway desktop icon

The selected icon is **B — Lift**, approved on September 29, 2026. It was generated with the built-in image-generation tool using the previous icon as a visual identity reference.

- `AppIcon-source.png`: approved Lift artwork with an opaque, full-bleed navy background. macOS applies the rounded corners.
- `AppIcon-source-original.png`: original artwork retained for reference.
- `AppIcon-source-lift-padded.png`: the first Lift export, retained for reference. Its transparent padding caused macOS 26 to add a gray outer tile; do not use for packaging.
- `AppIcon.icns`: generated macOS icon family for release packaging.
- `AppIcon-preview.png`: generated 1024-pixel preview.

`scripts/render-macos-icon.swift` preserves the complete composition and exports opaque RGB images at fixed pixel sizes, independent of display scaling. `make app` regenerates both the release icon and the Wails app icon before building. `make setup` builds and installs the app. This follows [Apple's guidance](https://developer.apple.com/design/human-interface-guidelines/app-icons) to provide an opaque, full-bleed background and let the system mask its corners.

## Full-bleed correction

Applied with the built-in image-generation tool, using the approved Lift icon as the edit target:

Use case: precise-object-edit. Edit the attached approved Rancher Runway Lift icon ONLY to fix macOS app-icon packaging.
Keep the central mint runway emblem EXACTLY as it is: same two tapered mint rails, same four white centerline dashes, same placement, same proportions, same colors, same sharpness. Keep the existing beautiful midnight-navy gradient and subtle upper-left blue illumination.
The sole change: remove the transparent outside padding and the rounded-square tile silhouette/rim. Extend the existing midnight-navy background smoothly all the way to EVERY outer edge and ALL FOUR CORNERS of the square canvas. The final image must be a full-bleed, fully opaque SQUARE background, with no transparency, no rounded corners, no inset tile, no perimeter outline, no outer border or shadow, and no gray/white frame. macOS will round the corners itself. Remove the old rounded tile boundary completely so there is only one continuous navy background. Do not add anything. Do not zoom or crop the central emblem. Do not redesign the mark. No text. This is the production background fix for the already-approved icon, not a new concept.

## Original generation prompt

Use case: logo-brand. Asset type: a single premium macOS desktop app icon concept for Rancher Runway, square image.
Input image 1 is a visual identity reference: a centered forward-reaching runway on midnight navy, green-cyan light. Create a distinct but recognizable simplified runway icon with stronger small-size legibility. This is direction B, Lift, not a presentation sheet.
A beautifully proportioned opaque dark ink-blue squircle. In its center a bold geometric runway emblem: two thick flat satin-mint tapered rails sweep straight upward from a wide base and converge toward a calm vanishing point, separated by a generous dark runway corridor. Three crisp softly white-mint centerline dashes shrink toward the apex, with a small fourth dash if needed. The full emblem fits inside the tile with generous balanced margins and has a beautifully clean silhouette. It feels like forward motion and a clear path, not the letter A or a triangular warning sign. Keep rails separate at their base and avoid any crossbar. Predominantly flat vector-like forms with only delicate satin depth and soft light from upper left. Saturated emerald at the bottom gently transitions to pale mint near the top. Deep navy tile with subtle corner falloff. No neon bloom, no haze, no photographic surface texture, no scenery, no horizon landscape. A precise, memorable application mark that survives at 24 to 32 pixels. Flat front-facing tile, not tilted. The opaque squircle occupies 90 percent of the square canvas, centered with even padding. Outside the tile is genuinely transparent. No text, labels, surrounding UI, additional icons or decorative symbols.
