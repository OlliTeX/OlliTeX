package validtools

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DatetimeOpts mirrors the options datetimeSchema() (zodHelpers.js) forwards
// to z.iso.datetime, plus the Node-side null handling, all pinned against
// installed zod 4.1.11 (v4/core/regexes.js, datetime()):
//
//	regex suffix alternation: opts = ["Z"];
//	    local  → adds the empty-string alternative (bare datetimes);
//	    offset → adds "([+-](?:[01]\d|2[0-3]):[0-5]\d)".
//	    The suffix group is REQUIRED — probed: bare "YYYY-MM-DDTHH:MM" fails.
//
//	union = [z.date(), z.iso.datetime(zodOptions)]
//	        (+ z.null() when nullable, + z.undefined() when nullish).
type DatetimeOpts struct {
	Offset     bool // accept a ±HH:MM numeric offset (Z always accepted)
	Local      bool // also accept bare (no-zone) datetimes
	AllowNull  bool // nullable arm: z.null()
	AllowUndef bool // nullish arm: z.undefined()
}

// DTSchema is the Go equivalent of `zz.datetime(opts)` / `datetimeNullable`
// / `datetimeNullish`: a value schema that the Field framework and the
// validateSchema port call via Validate(present, v).
type DTSchema struct {
	opts   DatetimeOpts
	nullar bool // nullable (allowNull)
	nullsh bool // nullish (allowNull + allowUndefined)
	re     *regexp.Regexp
}

// Datetime builds the strict schema.
func Datetime(opts DatetimeOpts) DTSchema {
	return DTSchema{opts: opts, re: buildDatetimeRegex(opts)}
}

// DatetimeNullable = Node datetimeNullable(): {...opts, allowNull: true}.
func DatetimeNullable(opts DatetimeOpts) DTSchema {
	opts.AllowNull = true
	return DTSchema{opts: opts, nullar: true, re: buildDatetimeRegex(opts)}
}

// DatetimeNullish = Node datetimeNullish(): {...opts, allowNull, allowUndefined}.
func DatetimeNullish(opts DatetimeOpts) DTSchema {
	opts.AllowNull = true
	opts.AllowUndef = true
	return DTSchema{opts: opts, nullar: true, nullsh: true, re: buildDatetimeRegex(opts)}
}

// DT is the normalised output of a datetime* parse, after the Node `.transform`:
// absent-ish inputs (absent, or null under nullish) collapse to Absent=true;
// an explicit null under nullable stays Null=true; anything else is the parsed
// instant (UTC-normalised — Go callers use time.Time directly; the Node side
// wraps it in a Date object, which is why jsTypeName(time.Time) == "date").
type DT struct {
	Absent bool
	Null   bool
	Time   time.Time
}

// Validate implements the Field.Val interface for the strict-object framework.
//
// Null/absent dispatch is oracle-pinned (Node 22, live probes):
//   - absent key  → undefined arm if present (nullish), else union type issue
//     "received undefined";
//   - explicit null → NULL arm whenever it exists (nullable AND nullish —
//     Node's transform collapses `null` to `null`, NOT undefined: only a
//     genuinely absent key collapses to undefined), else "received null".
func (d DTSchema) Validate(present bool, v any) (DT, []Issue) {
	if !present {
		if d.opts.AllowUndef {
			return DT{Absent: true}, nil
		}
		return DT{}, unionTypeIssue(d, "undefined")
	}
	if isNull(v) {
		if d.opts.AllowNull {
			return DT{Null: true}, nil
		}
		return DT{}, unionTypeIssue(d, "null")
	}
	if t, ok := v.(time.Time); ok {
		return DT{Time: t}, nil
	}
	if s, ok := v.(string); ok {
		// Probed (string fails the shape AND fails the regex for ALL arms at
		// once): a wrong-shaped string collapses to ONE invalid_format issue,
		// NOT an invalid_union.
		if !d.re.MatchString(s) {
			return DT{}, []Issue{NewIssue("invalid_format", "Invalid ISO datetime")}
		}
		return DT{Time: parseISODatetime(s)}, nil
	}
	return DT{}, unionTypeIssue(d, jsTypeName(v))
}

// unionTypeIssue builds the single invalid_union issue (empty path — the
// caller prepends the object key) whose per-arm groups are "received X"
// type issues. Probed: sub-issue paths are EMPTY; the union issue carries
// the object key.
func unionTypeIssue(d DTSchema, received string) []Issue {
	groups := [][]Issue{
		{NewIssue("invalid_type", "Invalid input: expected date, received "+received)},
		{NewIssue("invalid_type", "Invalid input: expected string, received "+received)},
	}
	if d.opts.AllowNull {
		groups = append(groups, []Issue{NewIssue("invalid_type", "Invalid input: expected null, received "+received)})
	}
	if d.opts.AllowUndef {
		groups = append(groups, []Issue{NewIssue("invalid_type", "Invalid input: expected undefined, received "+received)})
	}
	return []Issue{NewUnionIssue(groups)}
}

// DTAdapterVal adapts DTSchema to the Val interface (the DT return
// type vs the (any, []Issue) contract):
//
//	DT{Absent}   → (Absent(), nil)  absent/normalised-undefined key, dropped
//	DT{Null}     → (nil, nil)       explicit JSON null (nullable arm)
//	DT{Time}     → (time.Time)
//	issues       → (nil, issues)
//
// Wiring for the field side (docstore B2 uses Optional: true on the field
// that carries DatetimeNullable, matching Node zz.datetimeNullable which
// unions z.undefined()): absent keys are never passed to the schema there.
// A STRICT field (no Optional) wires this adapter directly, producing the
// `dt absent(strict)` goldens.
type DTAdapterVal struct{ DTSchema }

func (d DTAdapterVal) Validate(present bool, v any) (any, []Issue) {
	dt, is := d.DTSchema.Validate(present, v)
	if len(is) > 0 {
		return nil, is
	}
	if dt.Absent {
		return Absent(), nil
	}
	if dt.Null {
		return nil, nil
	}
	return dt.Time, nil
}

// buildDatetimeRegex mirrors zod 4.1.11's regexes.datetime():
//
//	^dateSource T timeSrc (?:Z | (when offset) [+-]HH:MM | (when local) "")$
func buildDatetimeRegex(opts DatetimeOpts) *regexp.Regexp {
	suffixes := []string{"Z"}
	if opts.Local {
		suffixes = append(suffixes, "")
	}
	if opts.Offset {
		suffixes = append(suffixes, `([+-](?:[01]\d|2[0-3]):[0-5]\d)`)
	}
	re := "^" + datetimeDateSrc + "T(?:" + datetimeTimeSrc + "(?:" +
		strings.Join(suffixes, "|") + "))$"
	return regexp.MustCompile(re)
}

// datetimeDateSrc is zod 4.1.11's dateSource verbatim (embeds the calendar:
// month 13, day 0, and 2023-02-29 never match — probed).
const datetimeDateSrc = `(?:(?:\d\d[2468][048]|\d\d[13579][26]|\d\d0[48]|[02468][048]00|[13579][26]00)-02-29|\d{4}-(?:(?:0[13578]|1[02])-(?:0[1-9]|[12]\d|3[01])|(?:0[469]|11)-(?:0[1-9]|[12]\d|30)|(?:02)-(?:0[1-9]|1\d|2[0-8])))`

// datetimeTimeSrc = (?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d(?:\.\d+)?)?.
const datetimeTimeSrc = `(?:[01]\d|2[0-3]):[0-5]\d(?::[0-5]\d(?:\.\d+)?)?`

// parseISODatetime converts a shape-validated datetime string to a UTC time
// (the Node `new Date(dt)` of the iso.datetime parse). Fractional seconds are
// dropped at millisecond precision — fine for the services' use (they store
// and compare instants, never re-serialise sub-ms).
func parseISODatetime(s string) time.Time {
	rest := s[11:] // everything after 'T'
	// zone: the trailing Z | +HH:MM | -HH:MM | "" — find its start.
	zoneStart := len(rest)
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c == 'Z' || c == '+' || c == '-' {
			zoneStart = i
			break
		}
	}
	zone := rest[zoneStart:]
	hmm := rest[:zoneStart]

	parts := strings.Split(hmm, ":")
	h, _ := strconv.Atoi(parts[0])
	m, sec := 0, 0
	if len(parts) >= 2 {
		m, _ = strconv.Atoi(parts[1])
	}
	if len(parts) == 3 {
		whole := parts[2]
		if dot := strings.Index(whole, "."); dot >= 0 {
			whole = whole[:dot]
		}
		sec, _ = strconv.Atoi(whole)
	}

	y, _ := strconv.Atoi(s[0:4])
	mo, _ := strconv.Atoi(s[5:7])
	d, _ := strconv.Atoi(s[8:10])

	loc := time.UTC
	if zone != "" && zone != "Z" {
		sign := 1
		if zone[0] == '-' {
			sign = -1
		}
		oh, _ := strconv.Atoi(zone[1:3])
		om, _ := strconv.Atoi(zone[4:6])
		loc = time.FixedZone("", sign*(oh*60+om)*60)
	} else if zone == "" {
		// local:true bare datetime: JS `new Date(str)` parses it as LOCAL
		// time; the Go services in this repo don't set Local, so this is a
		// fallback normalisation.
		loc = time.FixedZone("", 0)
	}
	// All results normalise to UTC (Date objects are instants; the Go
	// consumers in this repo compare and store instants, never tz-wall).
	return time.Date(y, time.Month(mo), d, h, m, sec, 0, loc).UTC()
}
