// Package regexes exists to encapsulate the regexes (i.e. make them read-only).
package regexes

import "regexp"

// These regexps are for matching against user input.
// We want to avoid stuff like the following, because it leads us to spam the nix-community.org server.
// - sub *
// - sub pythonPackages.*
//
// Unsubbing with the same queries is OK, because it it has different semantics and doesn't spam upstream.
var (
	dangerous = regexp.MustCompile(`^(?i:sub) (?:[*?]+|\w+\.\*)$`)
	subscribe = regexp.MustCompile(`^(?i:(un)?sub) ([\w_?*.-]+)$`)
	follow    = regexp.MustCompile(`^(?i:(un)?follow) (\w+)$`)
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

type normalization struct {
	pattern     *regexp.Regexp
	replacement string
}

// normalizations maps package set prefixes from packages.json to the canonical
// names used by nixpkgs-update. These normalizations can be found in:
// https://github.com/nix-community/infra/blob/1416f0bad404696b6cdeb7db2da6770abb3da2ac/hosts/build02/filter.sed
// Note that the regexes that just drop packages entirely don't need to be
// implemented, since those packages won't be in the `packages` table anyway.
var normalizations = []normalization{
	// beam: replace versioned with most-compatible (26)
	{regexp.MustCompile(`^beam\w*2[0-9]*Packages`), "beam26Packages"},
	// linux kernel: replace versioned generic kernels with unversioned
	{regexp.MustCompile(`^linuxKernel\.packages\.linux_[0-9_]+\.`), "linuxPackages."},
	// lua: replace versioned/jit variants with the version used by neovim
	{regexp.MustCompile(`^lua\w*Packages`), "lua51Packages"},
	// llvm: replace versioned with unversioned
	{regexp.MustCompile(`^llvmPackages_\w*`), "llvmPackages"},
	// ocaml: replace _latest with unversioned
	{regexp.MustCompile(`^ocamlPackages_latest`), "ocamlPackages"},
	// php extensions: replace versioned with unversioned
	{regexp.MustCompile(`^php\w*Extensions`), "phpExtensions"},
	// php packages: replace versioned with unversioned
	{regexp.MustCompile(`^php\w*Packages`), "phpPackages"},
	// postgresql: replace versioned/jit with unversioned
	{regexp.MustCompile(`^postgresql\w*Packages`), "postgresqlPackages"},
	// python3: replace versioned with unversioned
	{regexp.MustCompile(`^python3\w*Packages`), "python3Packages"},
}

func NormalizeAttrPath(ap string) string {
	for _, n := range normalizations {
		replaced := n.pattern.ReplaceAllLiteralString(ap, n.replacement)

		if replaced != ap {
			return replaced
		}
	}
	return ap
}
