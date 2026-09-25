package sharejstxtcomp

// Golden table captured live from the vendored Node oracle of
// app/js/sharejs/types/text-composable.js. Each row: input = JSON of the
// args; expected = result JSON (string payloads are quoted) or "ERR:<msg>".
type goldenRow struct {
	kind     string
	name     string
	input    string
	expected string
}

var goldenRows = []goldenRow{
	{"create", "create", "[]", "\"\""},
	{"normalize", "norm-merge", "[[1,{\"i\":\"a\"},{\"i\":\"b\"},{\"i\":\"\"},2,{\"i\":\"b\"}]]", "[1,{\"i\":\"ab\"},2,{\"i\":\"b\"}]"},
	{"normalize", "norm-dmerge", "[[{\"d\":\"a\"},{\"d\":\"b\"},{\"d\":\"\"},{\"i\":\"c\"}]]", "[{\"d\":\"ab\"},{\"i\":\"c\"}]"},
	{"apply", "apply-ins", "[\"Hello\",[1,{\"i\":\"X\"},4]]", "\"HXello\""},
	{"apply", "apply-d", "[\"Hello\",[3,{\"d\":\"lo\"}]]", "\"Hel\""},
	{"apply", "apply-skip", "[\"abc\",[3]]", "\"abc\""},
	{"apply", "apply-empty", "[\"ab\",[]]", "ERR:The applied op doesn't traverse the entire document"},
	{"apply", "apply-toolong", "[\"ab\",[5]]", "ERR:The op is too long for this document"},
	{"apply", "apply-mismatch", "[\"abcdef\",[1,{\"d\":\"xc\"}]]", "ERR:The deleted text 'xc' doesn't match the next characters in the document 'bc'"},
	{"apply", "apply-nottrav", "[\"abcdef\",[2]]", "ERR:The applied op doesn't traverse the entire document"},
	{"apply", "apply-nonstr", "[5,[]]", "ERR:Snapshot should be a string"},
	{"transform", "t-skip", "[[3],[3],\"left\"]", "[3]"},
	{"transform", "t-d-into-ins", "[[{\"d\":\"bc\"}],[{\"i\":\"b\"},2],\"right\"]", "[1,{\"d\":\"bc\"}]"},
	{"transform", "t-ins-ins-L", "[[{\"i\":\"ab\"}],[{\"i\":\"x\"}],\"left\"]", "[{\"i\":\"ab\"},1]"},
	{"transform", "t-ins-ins-R", "[[{\"i\":\"ab\"}],[{\"i\":\"x\"}],\"right\"]", "[1,{\"i\":\"ab\"}]"},
	{"transform", "t-empty", "[[],[],\"right\"]", "[]"},
	{"transform", "t-d-d-L", "[[{\"d\":\"abcd\"}],[2,{\"d\":\"c\"}],\"left\"]", "ERR:Remaining fragments in the op: undefined"},
	{"transform", "t-skip-part", "[[{\"i\":\"abc\"}],[1],\"right\"]", "ERR:The op traverses more elements than the document has"},
	{"transform", "t-tooLong", "[[{\"d\":\"ab\"}],[3],\"left\"]", "ERR:The op traverses more elements than the document has"},
	{"transform", "t-trailing", "[[3,{\"d\":\"x\"}],[1],\"left\"]", "ERR:Remaining fragments in the op: undefined"},
	{"transform", "t-adj-err", "[[1],[2,3],\"left\"]", "ERR:Adjacent skip components should be added"},
	{"transform", "t-numskip-adj", "[[{\"d\":\"a\"}],[1,2],\"right\"]", "ERR:Adjacent skip components should be added"},
	{"compose", "c-skip-copy", "[[3],[1,{\"i\":\"z\"},2]]", "[1,{\"i\":\"z\"},2]"},
	{"compose", "c-d-into-skip", "[[1,3],[{\"d\":\"ab\"}]]", "ERR:Adjacent skip components should be added"},
	{"compose", "c-d-eat-ins", "[[{\"i\":\"ab\"},3],[2,{\"d\":\"ab\"}]]", "ERR:Trailing stuff in op1 undefined"},
	{"compose", "c-d-skip", "[[3],[{\"d\":\"ab\"},1]]", "[{\"d\":\"ab\"},1]"},
	{"compose", "c-d-match", "[[{\"d\":\"ab\"}],[{\"d\":\"ab\"}]]", "ERR:The op traverses more elements than the document has"},
	{"compose", "c-mismatch", "[[{\"i\":\"a\"},{\"d\":\"b\"},2],[{\"d\":\"b\"},{\"d\":\"ab\"}]]", "ERR:The deleted text doesn't match the inserted text"},
	{"compose", "c-trailing-err", "[[3],[]]", "ERR:Trailing stuff in op1 undefined"},
	{"compose", "c-tooLong", "[[{\"d\":\"ab\"}],[1,{\"d\":\"x\"}]]", "ERR:The op traverses more elements than the document has"},
	{"compose", "c-empty", "[[],[]]", "[]"},
	{"compose", "c-ins-only", "[[2],[{\"i\":\"Q\"}]]", "ERR:Trailing stuff in op1 undefined"},
	{"invert", "i-mixed", "[[1,{\"i\":\"ab\"},{\"d\":\"cd\"}]]", "[1,{\"d\":\"ab\"},{\"i\":\"cd\"}]"},
	{"invert", "i-merge", "[[{\"i\":\"a\"},{\"i\":\"b\"},2,{\"d\":\"x\"},{\"d\":\"y\"}]]", "[{\"d\":\"ab\"},2,{\"i\":\"xy\"}]"},
	{"apply", "e-adj-skip", "[\"x\",[1,2]]", "ERR:Adjacent skip components should be added"},
	{"apply", "e-skip0", "[\"x\",[0]]", "ERR:Skip components must be a positive number"},
	{"apply", "e-notarray", "[\"x\",{\"i\":\"a\"}]", "ERR:Op must be an array of components"},
	{"apply", "e-badcomp", "[\"x\",[{\"i\":\"\",\"d\":\"\"}]]", "ERR:Invalid op component: undefined"},
}
