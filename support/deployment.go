package support

import "embed"

//go:embed Dockerfile .dockerignore go.mod go.sum cmd internal
var DeploymentFiles embed.FS
