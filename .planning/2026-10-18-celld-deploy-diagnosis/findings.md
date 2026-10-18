# Findings — 2026-10-18

- Existing session codex-20261018-153148, default socket, pane %0 is an authenticated SSH session. Harmless checks confirmed host/user marques512sv and directory /home/marques512sv/celld-mini-chat.
- Failed celld 0.6.2 deployment bundled the Worker and assets, then attempted PUT to AWS metadata 169.254.169.254/latest/api/token and timed out.
- Verified without exposing values: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION, AWS_PROFILE and AWS_SESSION_TOKEN were absent from the remote process environment.
- HTTP request to configured S3 endpoint http://192.168.76.1:9000/ returned 403. Endpoint is reachable; this unauthenticated response does not establish bucket readiness or credential validity.
- Remote `celld deploy --help` states credentials come from the standard AWS credential chain. Endpoint/bucket flags do not supply credentials.
- Frontend FleetDetailsModal.tsx shows the deploy command and copies only fleet.deploy.command. Reveal credentials / Copy environment separately supplies exports for the fleet access key, secret key and region. The instructions mention the prerequisite, but copying the command alone does not produce a complete deploy recipe.
- Backend master/services/celld.go emits the command separately from its credential endpoint and environment names. docs/celld.md explicitly requires exporting credentials before the command.
- Remediation: Deployment → Reveal credentials → Copy environment; execute those exports in this same remote shell, then rerun the deploy command. Do not paste credentials into chat. No authenticated deployment was run during diagnosis.
