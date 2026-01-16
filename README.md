# HyperHive

---

## **<a href="https://systems.hyperhive.maruqes.com/" target="_blank">→ VISIT THE OFFICIAL WEBSITE ←</a>**

**Documentation • Installation • Updates**

---

This README is intentionally brief. The full documentation lives on the website.
HyperHive is a purpose-built virtualization fabric for homelabs and small datacenters. It delivers the feel of a curated platform without the weight of enterprise stacks, keeping operations approachable while leaving plenty of room to scale.

## Why it is different
HyperHive focuses on predictable day-to-day ops rather than endless configuration. It is less heavy than typical cloud toolchains, yet powerful enough to run serious home infrastructure: clustered VMs, shared storage, and secure remote access with a single control plane. Full setup details live on the website.

## Architecture at a glance
- **Master node** runs the control plane, API, and UI. It owns cluster state and schedules actions.
- **Slave nodes** expose virtualization and system tasks back to the master over gRPC.
- **Secure transport** uses WireGuard for encrypted management traffic.
- **512rede overlay (optional)** provides isolated remote access with the master acting as a controlled gateway.

## Security
CrowdSec integration details are documented in `CROWDSEC.md`.
