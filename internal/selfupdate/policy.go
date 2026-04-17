package selfupdate

import "strings"

// UpdateDecision is the output of the policy check.
type UpdateDecision struct {
	ShouldUpdate bool
	// Reason is a short, user-facing explanation of the decision
	// (e.g. "already up to date", "newer release available").
	Reason string
}

// PolicyInput collects everything the policy needs to decide.
// Keeping this as a value (not separate args) makes the decision testable
// without plumbing the whole Options struct into unit tests.
type PolicyInput struct {
	// Current is the version embedded at build time (e.g. "v0.2.2" or "dev").
	Current string
	// Latest is the tag from GitHub (e.g. "v0.3.0", possibly a prerelease).
	Latest string
	// AllowPrerelease means the user opted into prerelease tags via --pre.
	AllowPrerelease bool
	// Force means "reinstall even if we're already on Latest" (--force).
	// Force does NOT downgrade — that's a separate concern we don't implement.
	Force bool
}

// decideUpdate applies the update policy.
//
// Rules:
//  1. Force overrides everything except prerelease filtering: if --force is
//     set and Latest is acceptable per --pre, we reinstall.
//  2. "dev" builds always update to any stable release (there's nothing
//     meaningful to compare against — dev has no version ordering).
//  3. Prereleases are skipped unless --pre is set.
//  4. Otherwise, update only when Latest is strictly greater than Current
//     per SemVer precedence.
func decideUpdate(in PolicyInput) (UpdateDecision, error) {
	latestIsPre := isPrerelease(in.Latest)
	if latestIsPre && !in.AllowPrerelease {
		return UpdateDecision{
			ShouldUpdate: false,
			Reason:       "latest release " + in.Latest + " is a prerelease; pass --pre to install prereleases",
		}, nil
	}

	if strings.EqualFold(in.Current, "dev") || in.Current == "" {
		return UpdateDecision{
			ShouldUpdate: true,
			Reason:       "current build is a development snapshot; installing release " + in.Latest,
		}, nil
	}

	current, err := parseSemver(in.Current)
	if err != nil {
		return UpdateDecision{}, err
	}
	latest, err := parseSemver(in.Latest)
	if err != nil {
		return UpdateDecision{}, err
	}

	cmp := compareSemver(current, latest)
	switch {
	case cmp < 0:
		return UpdateDecision{ShouldUpdate: true, Reason: "newer release available: " + in.Latest}, nil
	case cmp == 0:
		if in.Force {
			return UpdateDecision{ShouldUpdate: true, Reason: "already on " + in.Latest + " — reinstalling because --force was set"}, nil
		}
		return UpdateDecision{ShouldUpdate: false, Reason: "already on the latest release " + in.Latest}, nil
	default:
		return UpdateDecision{ShouldUpdate: false, Reason: "current version " + in.Current + " is newer than release " + in.Latest + "; refusing to downgrade"}, nil
	}
}
