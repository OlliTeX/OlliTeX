package compilemanager

import (
	"regexp"
	"strconv"

	latexmetrics "ollitex/go/services/clsitex/latexmetrics"
	clsl "ollitex/go/services/clsitex/logger"
	"ollitex/go/services/clsitex/metrics"
)

// KNOWN_LATEXMK_RULES mirrors the module-level set (rules the latexmk
// duration histogram knows by name).
var knownLatexmkRules = map[string]bool{
	"biber":     true,
	"bibtex":    true,
	"dvipdf":    true,
	"latex":     true,
	"lualatex":  true,
	"makeindex": true,
	"pdflatex":  true,
	"xdvipdfmx": true,
	"xelatex":   true,
}

// latexPassesRules mirrors LATEX_PASSES_RULES: rules that count as a pass.
var latexPassesRules = map[string]bool{
	"latex":    true,
	"lualatex": true,
	"xelatex":  true,
	"pdflatex": true,
}

var imageNameTagRe = regexp.MustCompile(`:(.*)`)

// isImageNameAllowed mirrors _isImageNameAllowed: every image is allowed
// when the allowlist is unset; otherwise the name must appear in it.
func (m *Manager) isImageNameAllowed(imageName string) bool {
	if len(m.AllowedImages) == 0 {
		return true
	}
	for _, allowed := range m.AllowedImages {
		if imageName == allowed {
			return true
		}
	}
	return false
}

type variantSettings struct {
	ImageName string
	Env       map[string]string
}

// getImageVariantSettings mirrors _getImageVariantSettings: checkpointing
// compiles run on the `<image>.checkpointing` variant when that image is
// allowed on this host; otherwise the requested image is returned as-is
// (with a logged error for the unexpected case).
func (m *Manager) getImageVariantSettings(req *Request) variantSettings {
	if req.EnableCheckpoint {
		checkpointingImageName := req.ImageName + ".checkpointing"
		if req.ImageName != "" && m.isImageNameAllowed(checkpointingImageName) {
			return variantSettings{
				ImageName: checkpointingImageName,
				Env:       map[string]string{"ENABLE_CHECKPOINT": "1"},
			}
		}
		clsl.Warn(map[string]any{
			"projectId": req.ProjectID,
			"userId":    req.UserID,
			"imageName": req.ImageName,
		}, "no checkpointing variant available for image, compiling without it")
	}
	return variantSettings{ImageName: req.ImageName, Env: map[string]string{}}
}

// f64 reads a numeric value out of a map[string]any flow map (flows store
// int64 ms values); missing/nil -> 0 (Node: undefined arithmetic yields
// NaN, which Go treats as 0 for our purposes — see compile.go docs).
func f64(v any) float64 {
	if n, ok := v.(float64); ok {
		return n
	}
	if n, ok := v.(int64); ok {
		return float64(n)
	}
	if n, ok := v.(int); ok {
		return float64(n)
	}
	return 0
}

// boolStr mirrors Node's `flag ? 'true' : 'false'` label strings.
func boolStr(flag bool) string {
	if flag {
		return "true"
	}
	return "false"
}

// emitMetrics mirrors _emitMetrics(request, status, stats, timings).
//
// stats is the flow's map[string]any: the "latexmk" sub-map holds typed
// RuleTime/ImgTime slices + the "latexmk-time" total (see
// latexmetrics.LatexMk). timings carry int64 millisecond values written by
// the compile flow. Node's shouldSkipMetrics(request) keys on
// request.metricsOpts.path — same here (m.SkipMetrics seam).
//
// Note: the clsi-perf gauge fires BEFORE the skip check (Node parity) —
// callers that only set compileE2E for perf requests must set it before the
// emit, matching Node where the gauge reads timings.compileE2E.
func (m *Manager) emitMetrics(req *Request, status string, stats, timings map[string]any) {
	if req.MetricsOpts.Path == "clsi-perf" {
		metrics.E2ECompileDurationClsiPerfSeconds.Set(map[string]string{
			"variant": req.MetricsOpts.Method,
		}, f64(timings["compileE2E"])/1000)
	}
	if m.SkipMetrics(req.MetricsOpts.Path) {
		return
	}

	// find the image tag to log it as a metric, e.g. 2015.1
	tag := "default"
	if req.ImageName != "" {
		if loc := imageNameTagRe.FindStringSubmatch(req.ImageName); loc != nil {
			tag = loc[1]
		}
	}

	lm := latexmetrics.LatexMk(stats)
	ruleTimes, _ := lm["latexmk-rule-times"].([]latexmetrics.RuleTime)
	passes := 0
	if ruleTimes != nil {
		var cumulativeRuleTimeMs float64
		for _, run := range ruleTimes {
			if latexPassesRules[run.Rule] {
				passes += 1
			}
			rule := "other"
			if knownLatexmkRules[run.Rule] {
				rule = run.Rule
			}
			metrics.LatexmkRuleDurationSeconds.Observe(map[string]string{
				"group": req.CompileGroup,
				"rule":  rule,
			}, run.TimeMS/1000)
			cumulativeRuleTimeMs += run.TimeMS
		}
		if total, ok := lm["latexmk-time"].(map[string]any); ok {
			totalMs, _ := total["total"].(float64)
			metrics.LatexmkRuleDurationSeconds.Observe(map[string]string{
				"group": req.CompileGroup, "rule": "overhead",
			}, (totalMs-cumulativeRuleTimeMs)/1000)
		}
	}

	if imgTimings, ok := lm["latexmk-img-times"].([]latexmetrics.ImgTime); ok {
		for _, timing := range imgTimings {
			metrics.ImageProcessingDurationSeconds.Observe(map[string]string{
				"group": req.CompileGroup,
				"type":  timing.Type,
			}, timing.TimeMS/1000)
		}
	}

	metrics.CompilesTotal.Inc(map[string]string{
		"status":              status,
		"engine":              req.Compiler,
		"image":               tag,
		"compile":             req.MetricsOpts.Compile,
		"group":               req.CompileGroup,
		"draft":               boolStr(req.Draft),
		"stop_on_first_error": boolStr(req.StopOnFirstError),
		"passes":              passesStr(passes),
		"type":                req.SyncType,
		"png2pdf":             boolStr(req.Png2pdf),
	})

	if v, ok := timings["sync"]; ok {
		metrics.SyncResourcesDurationSeconds.Observe(map[string]string{
			"type":    req.SyncType,
			"compile": req.MetricsOpts.Compile,
			"group":   req.CompileGroup,
		}, f64(v)/1000)
	}

	if v, ok := timings["compile"]; ok {
		metrics.CompileDurationSeconds.Observe(map[string]string{
			"status":  status,
			"engine":  req.Compiler,
			"compile": req.MetricsOpts.Compile,
			"group":   req.CompileGroup,
			"passes":  passesCategory(passes),
		}, f64(v)/1000)
	}

	if v, ok := timings["output"]; ok {
		metrics.ProcessOutputFilesDurationSeconds.Observe(map[string]string{
			"compile": req.MetricsOpts.Compile,
			"group":   req.CompileGroup,
		}, f64(v)/1000)
	}

	if v, ok := timings["compileE2E"]; ok {
		metrics.E2ECompileDurationSeconds.Observe(map[string]string{
			"compileFromHistory": boolStr(req.IsCompileFromHistory),
			"compile":            req.MetricsOpts.Compile,
			"group":              req.CompileGroup,
		}, f64(v)/1000)
	}
}

// passesStr mirrors Node passing a bare number as the `passes` label
// (prom auto-serializes); Go strings it.
func passesStr(passes int) string { return strconv.Itoa(passes) }

// passesCategory mirrors Node's `passes === 0 ? 'none' : passes === 1 ?
// 'single' : 'multiple'` label for compileDurationSeconds.
func passesCategory(passes int) string {
	switch {
	case passes == 0:
		return "none"
	case passes == 1:
		return "single"
	}
	return "multiple"
}
