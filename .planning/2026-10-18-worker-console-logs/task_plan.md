# Live celld log streaming

## Goal
Stream application/process logs into the terminal with authenticated connections and bounded recovery.

## Phases
- [x] Add notification-based agent gRPC tail and durable master fanout.
- [x] Add authenticated SSE with receipt-time replay and slow-reader handling.
- [x] Connect frontend follow using streaming fetch, cancellation and reconnect.
- [x] Test boundaries, build, document latency and deployment requirements.

## Next Step
Ready locally. Full low-latency rollout requires updated agents, master, frontend and fleet process restart for the250ms OTLP setting. SilverBullet proposal remains pending user preference.
