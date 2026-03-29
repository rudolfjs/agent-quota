// Package cli provides CLI command definitions, flag parsing,
// and output mode resolution.
package cli

// OutputMode determines how results are rendered.
type OutputMode int

const (
	OutputAuto   OutputMode = iota
	OutputPretty            // force TUI
	OutputJSON              // force JSON / headless
	OutputText              // plain text
)

// String returns the human-readable name of the output mode.
func (m OutputMode) String() string {
	switch m {
	case OutputAuto:
		return "auto"
	case OutputPretty:
		return "pretty"
	case OutputJSON:
		return "json"
	case OutputText:
		return "text"
	default:
		return "unknown"
	}
}

// ResolveOutputMode determines output mode from flags and TTY state.
//
// Rules (evaluated in priority order):
//   - json=true -> OutputJSON (regardless of anything else)
//   - pretty=true -> OutputPretty (regardless of TTY)
//   - provider set -> OutputJSON
//   - isTTY=true -> OutputPretty
//   - isTTY=false -> OutputJSON (fallback when not a terminal)
func ResolveOutputMode(provider string, pretty bool, jsonFlag bool, isTTY bool) OutputMode {
	switch {
	case jsonFlag:
		return OutputJSON
	case pretty:
		return OutputPretty
	case provider != "":
		return OutputJSON
	case isTTY:
		return OutputPretty
	default:
		return OutputJSON
	}
}
