package validtools

import (
	"testing"
	"time"
)

var (
	dtStrictRe = buildDatetimeRegex(DatetimeOpts{})
	dtLocalRe  = buildDatetimeRegex(DatetimeOpts{Local: true})
	dtOffsetRe = buildDatetimeRegex(DatetimeOpts{Offset: true})
)

func TestDatetimeShape(t *testing.T) {
	cases := []struct {
		re reFor
		in string
		ok bool
	}{
		{reStrict, "2024-01-01T12:00:00Z", true},
		{reStrict, "2024-01-01T12:00Z", true},
		{reStrict, "2024-01-01T12:00:00.123Z", true},
		{reStrict, "2024-01-01T12:00:00.123456789Z", true},
		{reStrict, "2024-01-01T12:00:00", false},  // suffix REQUIRED
		{reStrict, "2024-01-01T12:00:00z", false}, // lowercase z
		{reStrict, "2024-01-01t12:00:00Z", false},
		{reStrict, "2024-01-31T23:59:59Z", true},
		{reStrict, "2024-02-29T12:00:00Z", true},  // leap ok
		{reStrict, "2023-02-29T12:00:00Z", false}, // leap fail
		{reStrict, "2024-13-01T12:00:00Z", false}, // month 13
		{reStrict, "2024-01-01T24:00:00Z", false}, // hour 24
		{reStrict, "2024-01-01T12:60:00Z", false}, // minute 60
		{reStrict, "2024-01-01T12:00:60Z", false}, // second 60
		{reStrict, "2000-02-29T00:00:00Z", true},  // century leap 2000
		{reStrict, "1900-02-29T00:00:00Z", false}, // 1900 not a leap year
		{reStrict, "2024-06-15T10:30:00+05:30", false},
		{reLocal, "2024-06-15T10:30:00", true},
		{reLocal, "2024-06-15T10:30Z", true},
		{reOffset, "2024-06-15T10:30:00+05:30", true},
		{reOffset, "2024-06-15T10:30:00-11:30", true},
		{reOffset, "2024-06-15T10:30:00-23:59", true}, // oracle accepts hh up to 23
		{reOffset, "2024-06-15T10:30:00+05:60", false},
		{reOffset, "2024-06-15T10:30:00Z", true},
		{reOffset, "2024-06-15T10:30:00", false},
	}
	for i, c := range cases {
		if got := c.re.match(c.in); got != c.ok {
			t.Fatalf("shape case %d (%q): got %v, want %v", i, c.in, got, c.ok)
		}
	}
}

type reFor struct {
	name  string
	match func(string) bool
}

var (
	reStrict = reFor{"strict", dtStrictRe.MatchString}
	reLocal  = reFor{"local", dtLocalRe.MatchString}
	reOffset = reFor{"offset", dtOffsetRe.MatchString}
)

func TestDatetimeValidate(t *testing.T) {
	strict := Datetime(DatetimeOpts{})
	nullable := DatetimeNullable(DatetimeOpts{})
	nullish := DatetimeNullish(DatetimeOpts{})
	var nullVal any
	wire := func(name string, iss []Issue, key string) string {
		if len(iss) == 0 {
			return "OK"
		}
		return mapIssue(iss[0].At(StrSeg(key)))
	}
	cases := []struct {
		name string
		sch  DTSchema
		pres bool
		v    any
		want string
	}{
		{"absent strict", strict, false, nil,
			`Invalid input: expected date, received undefined at "dt" or Invalid input: expected string, received undefined at "dt"`},
		{"absent nullable", nullable, false, nil,
			`Invalid input: expected date, received undefined at "dt" or Invalid input: expected string, received undefined at "dt" or Invalid input: expected null, received undefined at "dt"`},
		{"absent nullish", nullish, false, nil, "ABSENT"},
		{"null strict", strict, true, nullVal,
			`Invalid input: expected date, received null at "dt" or Invalid input: expected string, received null at "dt"`},
		{"null nullable", nullable, true, nullVal, "NULL"},
		// Oracle-pinned: an EXPLICIT null under nullish is PRESERVED as null
		// (Node transform: `if (allowNull && !dt) return dt === null ? null : undefined`);
		// only a genuinely ABSENT key collapses to undefined (see "absent nullish").
		{"null nullish preserved", nullish, true, nullVal, "NULL"},
		{"bad string strict", strict, true, "garbage", `Invalid ISO datetime at "dt"`},
		{"number strict", strict, true, float64(5),
			`Invalid input: expected date, received number at "dt" or Invalid input: expected string, received number at "dt"`},
		{"object strict", strict, true, map[string]any{"a": 1},
			`Invalid input: expected date, received object at "dt" or Invalid input: expected string, received object at "dt"`},
		{"array nullish", nullish, true, []any{1},
			`Invalid input: expected date, received array at "dt" or ` +
				`Invalid input: expected string, received array at "dt" or ` +
				`Invalid input: expected null, received array at "dt" or ` +
				`Invalid input: expected undefined, received array at "dt"`},
		{"bool nullish", nullish, true, true,
			`Invalid input: expected date, received boolean at "dt" or ` +
				`Invalid input: expected string, received boolean at "dt" or ` +
				`Invalid input: expected null, received boolean at "dt" or ` +
				`Invalid input: expected undefined, received boolean at "dt"`},
		{"bad string nullish", nullish, true, "2024-01-01T12:00:00", `Invalid ISO datetime at "dt"`},
	}
	for i, c := range cases {
		dt, iss := c.sch.Validate(c.pres, c.v)
		switch c.want {
		case "ABSENT":
			if !dt.Absent || len(iss) != 0 {
				t.Fatalf("case %d: dt=%+v iss=%v", i, dt, iss)
			}
			continue
		case "NULL":
			if !dt.Null || len(iss) != 0 {
				t.Fatalf("case %d: dt=%+v iss=%v", i, dt, iss)
			}
			continue
		default:
			got := wire("x", iss, "dt")
			if got != c.want {
				t.Fatalf("case %d (%s):\n got %q\nwant %q", i, c.name, got, c.want)
			}
		}
	}
}

func TestDatetimeOffsets(t *testing.T) {
	ok := Datetime(DatetimeOpts{Offset: true})
	if _, iss := ok.Validate(true, "2024-06-15T10:30:00+05:30"); len(iss) != 0 {
		t.Fatalf("offset +05:30: unexpected %v", iss)
	}
	if _, iss := ok.Validate(true, "2024-06-15T10:30:00-05:30"); len(iss) != 0 {
		t.Fatalf("offset -05:30: unexpected %v", iss)
	}
	if _, iss := ok.Validate(true, "2024-01-01T12:00:00"); len(iss) == 0 {
		t.Fatalf("bare datetime with offset:true should fail (no local)")
	}
}

func TestParseISODatetime(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
	}{
		{"2024-01-01T12:00:00Z", time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
		{"2024-01-01T12:00Z", time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
		{"2024-06-15T10:30:00+05:30", time.Date(2024, 6, 15, 5, 0, 0, 0, time.UTC)},
		{"2024-06-15T10:30:00-11:30", time.Date(2024, 6, 15, 22, 0, 0, 0, time.UTC)},
		{"2024-12-31T23:59:59Z", time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)},
	}
	for i, c := range cases {
		if got := parseISODatetime(c.in); got != c.want {
			t.Fatalf("case %d: parse(%s) = %v, want %v", i, c.in, got, c.want)
		}
	}
}
