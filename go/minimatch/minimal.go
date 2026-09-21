package minimatch

// Option flags — mirrors `MinimatchOptions` in src/index.ts (upstream
// minimatch 10.2.6, git@github.com:isaacs/minimatch, BlueOak-1.0.0).
//
// Every option the upstream type declares is carried here so that any field
// a caller sets on the Go struct matches the upstream default semantics.
// The platform-specific (windows/UNC) options are accepted but inert on
// Linux/POSIX; see README.md ("Divergences").
type Options struct {
	// nobrace: do not expand {x,y
	Nobrace bool
	// nocomment: do not treat patterns starting with '#' as a comment
	Nocomment bool
	// nonegate: do not treat patterns starting with '!' as negation
	Nonegate bool
	// debug: no effect (kept for parity)
	Debug bool
	// noglobstar: treat '**' the same as '*'
	Noglobstar bool
	// noext: do not expand extglobs like +(a|b)
	Noext bool
	// noextglob: (upstream alias name) same as Noext
	Noextglob bool
	// nonull: "return the pattern if nothing matches" (only used by MatchList)
	Nonull bool
	// windowsPathsNoEscape: "\\" is a path separator, not an escape char
	WindowsPathsNoEscape bool
	// AllowWindowsEscape: alias of WindowsPathsNoEscape (inverted upstream)
	AllowWindowsEscape *bool
	// partial: compare a partial path to a pattern
	Partial bool
	// dot: allow patterns to match when the *string* starts with "."
	Dot bool
	// nocase: ignore case
	Nocase bool
	// nocaseMagicOnly: ignore case only in wildcard patterns
	NocaseMagicOnly bool
	// magicalBraces: consider braces to be "magic"
	MagicalBraces bool
	// MatchBase: if set, patterns without slashes match the basename
	MatchBase bool
	// FlipNegate: if set, the return is flipped
	FlipNegate bool
	// OptimizationLevel: pattern preprocessing level (0, 1 [default], >=2)
	OptimizationLevel int
	// PreserveMultipleSlashes: "a/b///c" is valid, not "a/b/c"
	PreserveMultipleSlashes bool
	// WindowsNoMagicRoot: consider leading "a:" to be non-magic
	WindowsNoMagicRoot *bool
	// Platform: override defaultPlatform ('win32' or 'posix')
	Platform string
	// BraceExpandMax: limit for brace expansion (default = no limit)
	BraceExpandMax int
	// MaxExtglobRecursion: extglob depth before switching to noext mode
	MaxExtglobRecursion int
	// MaxGlobstarRecursion: depth limit for globstar body sections
	MaxGlobstarRecursion int
	// PlatformSep: path separator (default '/'; kept for parity)
	PlatformSep string
}

const defaultPlatform = "posix"

func (o *Options) extglobEnabled() bool { return !o.Noext && !o.Noextglob }

func (o *Options) windowsEscape() bool {
	if o.AllowWindowsEscape != nil {
		// upstream: allowWindowsEscape === false forces the flag ON
		return o.WindowsPathsNoEscape || *o.AllowWindowsEscape == false
	}
	return o.WindowsPathsNoEscape
}

func (o *Options) optimizationLevel() int {
	if o.OptimizationLevel == 0 {
		// upstream default is 1 (??-operator semantics: `optimizationLevel ?? 1`)
		return 1
	}
	return o.OptimizationLevel
}
