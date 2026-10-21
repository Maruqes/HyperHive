package safety

func (l Level) Valid() bool { return l == Low || l == Medium || l == High || l == VeryHigh }

func (l Level) RequiredAction() string {
	switch l {
	case Low:
		return "execute"
	case Medium:
		return "recorded_preflight"
	default:
		return "human_approval"
	}
}

func (l Level) Instruction() string {
	switch l {
	case Low:
		return "Run after normal validation."
	case Medium:
		return "Double-check the target, exact arguments and expected impact before calling; the server validates and records this preflight without a human prompt."
	case High:
		return "Explicit human approval is mandatory for each invocation, including in Approve for me and Ask for Approval."
	default:
		return "Very high impact: review disruption, possible data loss and recovery limits. Explicit human approval is mandatory for each invocation in both chat modes."
	}
}
