// Package requestparser mirrors services/clsi/app/js/RequestParser.js.
//
// Node's parse(body, callback) normalizes the compile-request JSON body and
// validates the compile options. Attribute names, defaults, type checks,
// regex checks and error messages are byte-for-byte per the Node source and
// its unit tests: the Go service is a drop-in, so the wire contract is
// identical.
package requestparser

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxTimeout mirrors RequestParser.MAX_TIMEOUT (seconds, pre-ms-conversion).
const MaxTimeout = 600

var (
	validCompilers = []string{"pdflatex", "latex", "xelatex", "lualatex"}
	syncTypes      = []string{"full", "incremental", "history-full", "history-incremental"}
)

var (
	// EditorIDRegex: /^[a-f0-9-]{36}$/ (UUID).
	EditorIDRegex = regexp.MustCompile(`^[a-f0-9-]{36}$`)
	// HistoryIDRegex: /^([0-9a-f]{24}|[1-9][0-9]{0,9})$/.
	HistoryIDRegex = regexp.MustCompile(`^([0-9a-f]{24}|[1-9][0-9]{0,9})$`)
	// BuildRegex mirrors OutputCacheManager.BUILD_REGEX: /^[0-9a-f]+-[0-9a-f]+$/.
	BuildRegex = regexp.MustCompile(`^[0-9a-f]+-[0-9a-f]+$`)
)

// ParsedResource mirrors a response.resources element.
type ParsedResource struct {
	Path        string
	Modified    interface{}
	URL         interface{}
	FallbackURL interface{}
	Content     interface{}
}

// MetricsOpts mirrors response.metricsOpts (compile is "initial" at parse
// time; CompileManager updates it later, as in Node).
type MetricsOpts struct {
	Path    string
	Method  string
	Compile string
}

// ParsedResponse is the normalized compile request (parse() response).
// Fields Node never assigns stay at their Go zero value (undefined in JS).
type ParsedResponse struct {
	MetricsOpts            MetricsOpts
	Compiler               string
	CompileFromClsiCache   bool
	PopulateClsiCache      bool
	EnablePdfCaching       bool
	PdfCachingMinChunkSize interface{}
	EnableCheckpoint       bool
	Timeout                int // ms
	ImageName              interface{}
	Draft                  bool
	Png2pdf                bool
	StopOnFirstError       bool
	Check                  interface{}
	Flags                  interface{}
	CompileGroup           *string
	SyncType               interface{}
	SyncState              interface{}
	Resources              []ParsedResource
	HistoryID              interface{}
	BaseHistoryVersion     interface{}
	GlobalBlobs            interface{}
	FilestoreBlobPrefix    interface{}
	ClSIPerfVariant        interface{}
	RootResourcePath       string
	EditorID               interface{}
	BuildID                interface{}
	RawSnapshot            interface{}
	RawChangeOperations    interface{}
	IsCompileFromHistory   bool
}

// Config carries the settings RequestParser depends on.
type Config struct {
	PdfCachingMinChunkSize  interface{}
	AllowedImages           []string
	AllowedCompileGroups    []string
	AllowedCompileGroupsSet bool
}

// ParseError mirrors a plain `Error` thrown inside RequestParser.parse.
type ParseError struct {
	Message string
}

func (e *ParseError) Error() string { return e.Message }

func parseErr(msg string) error { return &ParseError{Message: msg} }

func join(vv []string, sep string) string { return strings.Join(vv, sep) }

func isStr(v interface{}) bool  { _, ok := v.(string); return ok }
func isBool(v interface{}) bool { _, ok := v.(bool); return ok }
func isNum(v interface{}) bool {
	switch v.(type) {
	case float64, float32, int, int32, int64:
		return true
	}
	return false
}
func isArr(v interface{}) bool { _, ok := v.([]interface{}); return ok }
func isObj(v interface{}) bool { _, ok := v.(map[string]interface{}); return ok }
func has(v interface{}) bool   { return v != nil }

// parseStringNoDefault mirrors _parseAttribute(name, attr, {type:'string'}).
// Returns the Go zero value "" when attr is undefined (JS undefined).
func parseStringNoDefault(name string, v interface{}) (string, error) {
	if has(v) {
		if !isStr(v) {
			return "", parseErr(name + " attribute should be a string")
		}
		return v.(string), nil
	}
	return "", nil
}

// parseStringDefault mirrors _parseAttribute(name, attr, {default, type:'string'}).
func parseStringDefault(name string, v interface{}, def string) (string, error) {
	if has(v) {
		if !isStr(v) {
			return "", parseErr(name + " attribute should be a string")
		}
		return v.(string), nil
	}
	return def, nil
}

func parseBoolAttr(name string, v interface{}, def bool) (bool, error) {
	if has(v) {
		if !isBool(v) {
			return false, parseErr(name + " attribute should be a boolean")
		}
		return v.(bool), nil
	}
	return def, nil
}

// parseValidStringAttr mirrors _parseAttribute(name, attr,
// {validValues, type: 'string'[, default]}). JS checks
// validValues.indexOf FIRST (before typeof), so any non-string value
// (indexOf -1) lands in the 'one of' branch as well.
func parseValidStringAttr(name string, v interface{}, valid []string, def string) (string, error) {
	if has(v) {
		s, isStrV := v.(string)
		found := false
		if isStrV {
			for _, val := range valid {
				if val == s {
					found = true
					break
				}
			}
		}
		if !found {
			return "", parseErr(name + " attribute should be one of: " + join(valid, ", "))
		}
		return s, nil
	}
	return def, nil
}

func checkPath(p string) error {
	for _, dir := range strings.Split(p, "/") {
		if dir == ".." {
			return parseErr("relative path in root resource")
		}
	}
	return nil
}

func parseResource(resource map[string]interface{}) (ParsedResource, error) {
	var pres ParsedResource

	path, hasPath := resource["path"]
	if !hasPath || !isStr(path) {
		return pres, parseErr("all resources should have a path attribute")
	}
	pres.Path = path.(string)

	modified, hasModified := resource["modified"]
	if hasModified && has(modified) {
		ms, ok := modifiedEpoch(modified)
		if !ok {
			return pres, fmtErr("resource modified date could not be understood: %v", modified)
		}
		// V8: resource.modified becomes a Date object; for JSON response
		// parity we keep the numeric epoch-milliseconds value.
		pres.Modified = ms
	}

	urlHas, contentHas := has(resource["url"]), has(resource["content"])
	if !urlHas && !contentHas {
		return pres, parseErr("all resources should have either a url or content attribute")
	}
	if contentHas {
		c, _ := resource["content"]
		if !isStr(c) {
			return pres, parseErr("content attribute should be a string")
		}
		pres.Content = c
	}
	if urlHas {
		u, _ := resource["url"]
		if !isStr(u) {
			return pres, parseErr("url attribute should be a string")
		}
		pres.URL = u
	}
	if fu, hasfu := resource["fallbackURL"]; hasfu && has(fu) {
		if !isStr(fu) {
			return pres, parseErr("fallbackURL attribute should be a string")
		}
		pres.FallbackURL = fu
	}

	return pres, nil
}

// dateUnderstandable / the V8 port live in v8date.go.

func fmtErr(format string, args ...interface{}) error {
	return parseErr(fmt.Sprintf(format, args...))
}

// Parse mirrors RequestParser.parse(body, cb). body is the decoded JSON body.
func Parse(body map[string]interface{}, cfg Config) (ParsedResponse, error) {
	var response ParsedResponse

	compileRaw, hasCompile := body["compile"]
	// JS: body.compile == null (absent => undefined == null is true).
	if !hasCompile || compileRaw == nil {
		return response, parseErr("top level object should have a compile attribute")
	}
	compile, ok := compileRaw.(map[string]interface{})
	if !ok {
		return response, parseErr("top level object should have a compile attribute")
	}

	// JS: if (!compile.options) compile.options = {}
	// A missing/null/non-object options still yields undefined
	// attributes for every lookup, so treat it as an empty object.
	optionsRaw, isOptMap := compile["options"].(map[string]interface{})
	if !isOptMap || optionsRaw == nil {
		optionsRaw = map[string]interface{}{}
	}
	options := optionsRaw

	response.MetricsOpts.Compile = "initial"
	metricsPath, hasMP := options["metricsPath"]
	if hasMP {
		s, err := parseStringNoDefault("metricsPath", metricsPath)
		if err != nil {
			return response, err
		}
		response.MetricsOpts.Path = s
	}
	metricsMethod, hasMM := options["metricsMethod"]
	if hasMM {
		s, err := parseStringNoDefault("metricsMethod", metricsMethod)
		if err != nil {
			return response, err
		}
		response.MetricsOpts.Method = s
	}

	compiler, err := parseValidStringAttr("compiler", options["compiler"], validCompilers, "pdflatex")
	if err != nil {
		return response, err
	}
	response.Compiler = compiler

	compileFromClsiCache, err := parseBoolAttr("compileFromClsiCache", options["compileFromClsiCache"], false)
	if err != nil {
		return response, err
	}
	response.CompileFromClsiCache = compileFromClsiCache
	populateClsiCache, err := parseBoolAttr("populateClsiCache", options["populateClsiCache"], false)
	if err != nil {
		return response, err
	}
	response.PopulateClsiCache = populateClsiCache
	enablePdfCaching, err := parseBoolAttr("enablePdfCaching", options["enablePdfCaching"], false)
	if err != nil {
		return response, err
	}
	response.EnablePdfCaching = enablePdfCaching

	pmcRaw, hasPMC := options["pdfCachingMinChunkSize"]
	if hasPMC && pmcRaw != nil {
		if !isNum(pmcRaw) {
			return response, parseErr("pdfCachingMinChunkSize attribute should be a number")
		}
		response.PdfCachingMinChunkSize = pmcRaw
	} else {
		response.PdfCachingMinChunkSize = cfg.PdfCachingMinChunkSize
	}

	enableCheckpoint, err := parseBoolAttr("enableCheckpoint", options["enableCheckpoint"], false)
	if err != nil {
		return response, err
	}
	response.EnableCheckpoint = enableCheckpoint

	timeoutRaw, hasTimeout := options["timeout"]
	var timeoutVal float64
	if hasTimeout && timeoutRaw != nil {
		if !isNum(timeoutRaw) {
			return response, parseErr("timeout attribute should be a number")
		}
		timeoutVal = toFloat(timeoutRaw)
	} else {
		timeoutVal = float64(MaxTimeout)
	}
	// JS: if (response.timeout > MAX_TIMEOUT) response.timeout = MAX_TIMEOUT
	// (seconds compare, then converted to milliseconds below).
	if timeoutVal > float64(MaxTimeout) {
		timeoutVal = float64(MaxTimeout)
	}
	response.Timeout = int(timeoutVal * 1000)

	imageNameRaw, hasImageName := options["imageName"]
	if hasImageName && imageNameRaw != nil {
		if cfg.AllowedImages == nil {
			// Unrestricted: plain string attribute (typeof check only).
			if !isStr(imageNameRaw) {
				return response, parseErr("imageName attribute should be a string")
			}
			response.ImageName = imageNameRaw
		} else {
			// Restricted: JS checks validValues BEFORE typeof, so
			// non-strings (indexOf -1) hit 'one of' as well.
			iv, isImageStr := imageNameRaw.(string)
			found := false
			if isImageStr {
				for _, a := range cfg.AllowedImages {
					if a == iv {
						found = true
						break
					}
				}
			}
			if !found {
				return response, parseErr("imageName attribute should be one of: " + join(cfg.AllowedImages, ", "))
			}
			response.ImageName = imageNameRaw
		}
	}

	draft, err := parseBoolAttr("draft", options["draft"], false)
	if err != nil {
		return response, err
	}
	response.Draft = draft
	png2pdf, err := parseBoolAttr("png2pdf", options["png2pdf"], false)
	if err != nil {
		return response, err
	}
	response.Png2pdf = png2pdf
	stopOnFirstError, err := parseBoolAttr("stopOnFirstError", options["stopOnFirstError"], false)
	if err != nil {
		return response, err
	}
	response.StopOnFirstError = stopOnFirstError

	checkRaw, hasCheck := options["check"]
	if hasCheck {
		if !isStr(checkRaw) {
			return response, parseErr("check attribute should be a string")
		}
		response.Check = checkRaw
	}

	flagsRaw, hasFlags := options["flags"]
	if hasFlags && flagsRaw != nil {
		// JS typeof: arrays and plain objects BOTH report 'object'
		// (typeof [] === 'object'), so arrays must be accepted here.
		// (test: flags = ['-file-line-error']).
		if !isArr(flagsRaw) && !isObj(flagsRaw) {
			return response, parseErr("flags attribute should be an object")
		}
		response.Flags = flagsRaw
	} else {
		// Absent or explicit null -> JS default []
		response.Flags = []interface{}{}
	}

	if cfg.AllowedCompileGroupsSet {
		cgRaw, hasCG := options["compileGroup"]
		if hasCG && cgRaw != nil {
			// JS: validValues.indexOf runs first for any non-null
			// value, so non-strings hit the 'one of' error too.
			cg, isCgStr := cgRaw.(string)
			found := false
			if isCgStr {
				for _, val := range cfg.AllowedCompileGroups {
					if val == cg {
						found = true
						break
					}
				}
			}
			if !found {
				return response, parseErr("compileGroup attribute should be one of: " + join(cfg.AllowedCompileGroups, ", "))
			}
			g := cg
			response.CompileGroup = &g
		} else {
			// Default '' when settings.allowedCompileGroups is set.
			empty := ""
			response.CompileGroup = &empty
		}
	}

	syncTypeRaw, hasSyncType := options["syncType"]
	if hasSyncType && syncTypeRaw != nil {
		sts, isSTStr := syncTypeRaw.(string)
		found := false
		if isSTStr {
			for _, val := range syncTypes {
				if val == sts {
					found = true
					break
				}
			}
		}
		if !found {
			return response, parseErr("syncType attribute should be one of: full, incremental, history-full, history-incremental")
		}
		response.SyncType = syncTypeRaw
	}

	syncState, hasSyncState := options["syncState"]
	if hasSyncState && syncState != nil {
		if !isStr(syncState) {
			return response, parseErr("syncState attribute should be a string")
		}
		response.SyncState = syncState
	}

	resourcesRaw, hasResources := compile["resources"]
	if hasResources && has(resourcesRaw) {
		resources, ok := resourcesRaw.([]interface{})
		if !ok {
			// JS: compile.resources.map on a non-array throws a
			// TypeError (500 on the wire). Message mirrors V8.
			return response, parseErr("compile.resources.map is not a function")
		}
		for _, r := range resources {
			rm, ok := r.(map[string]interface{})
			if !ok {
				// JS: non-object resource -> resource.path is
				// undefined (== null) -> the path error below.
				return response, parseErr("all resources should have a path attribute")
			}
			pres, err := parseResource(rm)
			if err != nil {
				return response, err
			}
			response.Resources = append(response.Resources, pres)
		}
	}

	historyIdRaw, hasHistoryID := options["historyId"]
	if hasHistoryID && historyIdRaw != nil {
		if !isStr(historyIdRaw) {
			return response, parseErr("historyId attribute should be a string")
		}
		if !HistoryIDRegex.MatchString(historyIdRaw.(string)) {
			return response, parseErr("historyId attribute does not match regex " + "/^([0-9a-f]{24}|[1-9][0-9]{0,9})$/")
		}
		response.HistoryID = historyIdRaw
	}

	baseHistoryVersion, hasBHV := compile["baseHistoryVersion"]
	if hasBHV && baseHistoryVersion != nil {
		if !isNum(baseHistoryVersion) {
			return response, parseErr("baseHistoryVersion attribute should be a number")
		}
		response.BaseHistoryVersion = baseHistoryVersion
	}

	globalBlobs, hasGB := compile["globalBlobs"]
	if hasGB && globalBlobs != nil {
		if !isArr(globalBlobs) {
			return response, parseErr("globalBlobs attribute should be an array")
		}
		response.GlobalBlobs = globalBlobs
	}

	if rawSnapshot, hasRS := compile["rawSnapshot"]; hasRS {
		response.RawSnapshot = rawSnapshot
	}
	if rawChangeOps, hasRCO := compile["rawChangeOperations"]; hasRCO {
		response.RawChangeOperations = rawChangeOps
	}
	response.IsCompileFromHistory = has(compile["rawChangeOperations"])

	// JS: if (compile.filestoreBlobPrefix) — plain truthy check, no type
	// validation. V8: .split("/") exists on STRING (and number/boolean
	// boxing); on objects and arrays a TypeError is thrown.
	if fbpRaw, hasFBP := compile["filestoreBlobPrefix"]; hasFBP && fbpRaw != nil {
		if fbp, isStrFBP := fbpRaw.(string); isStrFBP {
			// Truthy only for non-empty strings; "" is falsy.
			if fbp != "" {
				if err := checkPath(fbp); err != nil {
					return response, err
				}
				response.FilestoreBlobPrefix = fbp
			}
		} else {
			// Objects and arrays: V8 throws `X.split is not a function`.
			return response, parseErr("compile.filestoreBlobPrefix.split is not a function")
		}
	}

	clsiPerfVariant, hasCPV := options["clsiPerfVariant"]
	if hasCPV && clsiPerfVariant != nil {
		if !isStr(clsiPerfVariant) {
			return response, parseErr("clsiPerfVariant attribute should be a string")
		}
		response.ClSIPerfVariant = clsiPerfVariant
	}

	rootResourcePathRaw := compile["rootResourcePath"]
	rootResourcePathVal, err := parseStringDefault("rootResourcePath", rootResourcePathRaw, "main.tex")
	if err != nil {
		return response, err
	}
	if err := checkPath(rootResourcePathVal); err != nil {
		return response, err
	}
	response.RootResourcePath = rootResourcePathVal

	editorId, hasED := options["editorId"]
	if hasED && editorId != nil {
		if !isStr(editorId) {
			return response, parseErr("editorId attribute should be a string")
		}
		if !EditorIDRegex.MatchString(editorId.(string)) {
			return response, parseErr("editorId attribute does not match regex " + "/^[a-f0-9-]{36}$/")
		}
		response.EditorID = editorId
	}

	buildId, hasBD := options["buildId"]
	if hasBD && buildId != nil {
		if !isStr(buildId) {
			return response, parseErr("buildId attribute should be a string")
		}
		if !BuildRegex.MatchString(buildId.(string)) {
			return response, parseErr("buildId attribute does not match regex " + "/^[0-9a-f]+-[0-9a-f]+$/")
		}
		response.BuildID = buildId
	}

	return response, nil
}

func toFloat(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int32:
		return float64(x)
	case int64:
		return float64(x)
	case float32:
		return float64(x)
	}
	return 0
}
