# Findings

Official https://go.dev/dl/?mode=json reports Go 1.27.3 stable on 2026-10-22. Master/slave select go1.26.6 and require Go 1.26.0. Five modules exist; master embeds the local mcp module. Build Makefile run/first targets start privileged services, so use passive Go checks.
