package regexes

import "regexp"

// These regexps are for matching against user input.
// We want to avoid stuff like the following, because it leads us to spam the nix-community.org server.
// - sub *
// - sub pythonPackages.*
//
// Unsubbing with the same queries is OK, because it it has different semantics and doesn't spam upstream.
var (
	dangerous = regexp.MustCompile(`^sub (?:[*?]+|\w+\.\*)$`)
	subscribe = regexp.MustCompile(`^(un)?sub ([\w_?*.-]+)$`)
	follow    = regexp.MustCompile(`^(un)?follow (\w+)$`)
)

// These two regexps are for parsing logs.
// - "error: " is a nix build error
// - "ExitFailure" is a nixpkgs-update error
// - "failed with" is a nixpkgs/maintainers/scripts/update.py error
var (
	error  = regexp.MustCompile(`^error:|ExitFailure|failed with`)
	ignore = regexp.MustCompile(`^~.*|^\.\.`)
)

func Dangerous() *regexp.Regexp {
	return dangerous
}

func Subscribe() *regexp.Regexp {
	return subscribe
}

func Follow() *regexp.Regexp {
	return follow
}

func Error() *regexp.Regexp {
	return error
}

func Ignore() *regexp.Regexp {
	return ignore
}
