package virsh

import (
	"fmt"
	"strings"
)

const irqBalanceUnitName = "irqbalance.service"

type IrqBalanceState struct {
	Enabled  bool
	Active   bool
	UnitName string
}

func GetIrqBalanceState() (*IrqBalanceState, error) {
	if !hasBinary("systemctl") {
		return nil, fmt.Errorf("systemctl is required")
	}

	out, err := runCmdOutputMaybeSudo(
		"systemctl",
		"show",
		irqBalanceUnitName,
		"--property=LoadState",
		"--property=UnitFileState",
		"--property=ActiveState",
	)
	if err != nil {
		return nil, err
	}

	props := parseSystemctlProperties(out)
	if strings.EqualFold(strings.TrimSpace(props["LoadState"]), "not-found") {
		return nil, fmt.Errorf("irqbalance service not found")
	}

	return &IrqBalanceState{
		Enabled:  isEnabledUnitFileState(props["UnitFileState"]),
		Active:   strings.EqualFold(strings.TrimSpace(props["ActiveState"]), "active"),
		UnitName: irqBalanceUnitName,
	}, nil
}

func SetIrqBalanceState(enabled bool) (*IrqBalanceState, error) {
	if !hasBinary("systemctl") {
		return nil, fmt.Errorf("systemctl is required")
	}

	if enabled {
		if err := runCmdDiscardOutputMaybeSudo("systemctl", "enable", "--now", irqBalanceUnitName); err != nil {
			return nil, err
		}
	} else {
		if err := runCmdDiscardOutputMaybeSudo("systemctl", "disable", "--now", irqBalanceUnitName); err != nil {
			return nil, err
		}
	}

	return GetIrqBalanceState()
}

func parseSystemctlProperties(out string) map[string]string {
	props := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		props[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return props
}

func isEnabledUnitFileState(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "enabled", "enabled-runtime", "linked", "linked-runtime", "alias":
		return true
	default:
		return false
	}
}
