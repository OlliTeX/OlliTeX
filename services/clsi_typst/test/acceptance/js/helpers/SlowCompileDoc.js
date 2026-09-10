// Slow compile document, for StopCompile/ClearCache acceptance tests.
//
// clsi makes the compile busy with a recursive \x macro (unbounded). Typst
// analog: a range long enough to keep the container running for the window
// the stop/clear requests land; it overflows ("value is too large") and
// errors with no PDF — which is fine, the compile response is still 200
// "terminated" and the compile dir is removed by clearCache.
// Calibrated against pandoc/typst:3-alpine (typst 0.14.2): runs ~28s and
// errors, rc=1, no output.pdf. Plenty of margin for the stop to land
// (~1-10s after compile start).
export default `\
#let s = range(0, 30000000).map(i => i * i).sum()
#s
`
