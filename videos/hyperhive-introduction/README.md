# HyperHive presentation video

A 60-second English introduction for homelab builders and small datacenter operators, built with HyperFrames 0.8.188 at 1920×1080, 30 fps.

- Delivery: `renders/hyperhive-introduction.mp4`
- Open editor: `npx hyperframes preview --background --port 3017`
- Validate: `npx hyperframes check --json`
- Render: `npx hyperframes render --quality delivery --output renders/hyperhive-introduction.mp4`

## Scenes
1. `s1-together` (0–10s): one control plane.
2. `s2-compute` (10–20s): VM creation, cloning, backup and migration.
3. `s3-storage` (20–30s): Btrfs, NFS and disk health.
4. `s4-network` (30–40s): proxies, certificates, DNS and WireGuard.
5. `s5-mcp` (40–50s): AI clients and supported MCP tool contracts.
6. `s6-identity` (50–60s): master, agents and HyperHive identity.

Short scene overlaps preserve full backgrounds during transitions. The root includes a matching opening headline to avoid a blank frame while the first sub-composition initializes.

Facts are grounded in the supplied repository and SilverBullet knowledge, with source boundaries in `findings.md`. The diagrams explain capabilities rather than showing a live deployment. Creative direction: `BRIEF.md`, `SPEC.md`, `STORYBOARD.md`, `SCRIPT.md`. Verification and limits: `REVIEW.md`. Music and asset credits: `CREDITS.md`; retain attribution when sharing the video.
