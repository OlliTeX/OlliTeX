package otc

// Ports libraries/overleaf-editor-core/lib/text_file_defaults.js 1:1.
//
// These lists decide whether a file becomes a doc or a binary file (see
// FileTypeErrorDetector), so every service that classifies a file must work
// from the same list. They are shared, and web layers an
// ADDITIONAL_TEXT_EXTENSIONS override on top for Server CE/Pro.
//
// Go has no Object.freeze, so callers must treat the returned slices as
// read-only and copy them before handing them to anything that merges
// configuration into its own objects (mirror the Node `.concat`/`.slice`
// guidance).

// DefaultTextExtensions are extensions (no leading dot, lower case) a file may
// use to be editable text.
var DefaultTextExtensions = []string{
	"tex",
	"latex",
	"sty",
	"cls",
	"bst",
	"bib",
	"bibtex",
	"txt",
	"tikz",
	"mtx",
	"rtex",
	"md",
	"asy",
	"lbx",
	"bbx",
	"cbx",
	"m",
	"lco",
	"dtx",
	"ins",
	"ist",
	"def",
	"clo",
	"ldf",
	"rmd",
	"qmd",
	"lua",
	"py",
	"gv",
	"mf",
	"yml",
	"yaml",
	"lhs",
	"lean",
	"lean4",
	"hs",
	"mk",
	"xmpdata",
	"cfg",
	"rnw",
	"ltx",
	"inc",
}

// DefaultRootDocExtensions are the extensions a doc may have to be the one a
// project compiles from. A subset of DefaultTextExtensions: every one of these
// is editable, and most of the editable ones cannot be a root doc.
var DefaultRootDocExtensions = []string{"tex", "Rtex", "ltx", "Rnw"}

// DefaultEditableFilenames are whole file names (lower case) that are editable
// whatever their extension says.
var DefaultEditableFilenames = []string{
	"latexmkrc",
	".latexmkrc",
	"makefile",
	"gnumakefile",
}
