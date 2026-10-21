// Package vmsafety retains VM-specific recovery artifacts.
package vmsafety

import "github.com/Maruqes/HyperHive/mcp/internal/safety"

type Risk = safety.Level
type Gate = safety.Gate

const (
	Low      = safety.Low
	Medium   = safety.Medium
	High     = safety.High
	VeryHigh = safety.VeryHigh
)
