# Android Firebase implementation: Discovery Notes
Date: 2026-10-28 · Goal: Resolve required implementation inputs for the Android Firebase notification plan.

## Summary / key decisions
- Android package: `com.anonymous.hyperhive`. Firebase client configuration must be imported dynamically after installation.
- The user requested continued implementation without an attached device; real-device checks remain release prerequisites.

## Q&A log

### Q1 — Stable Android application ID
- Captured: User selected `com.anonymous.hyperhive`.
- Flags: None for package identity.

## Open flags (pending input)
- Stable Android application ID confirmed: `com.anonymous.hyperhive`.
- Real-device token, cold-start and delivery checks -> release verification; no device is available in this development session.

### Q2 — Device feasibility gate
- Captured: User cannot connect a device and wants development to continue with dynamic, post-installation google-services.json import.
- Decision: Continue implementation; physical-device token, cold-start and delivery checks remain unverified release checks.
