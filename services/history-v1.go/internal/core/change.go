package core

import (
	"encoding/json"
	"fmt"
	"time"
)

// Change — ports change.js. Wire (Node Change.toRaw, object; key order is not
// significant under parse-equivalence):
//
//	{
//	  operations:   [ <op toRaw>... ],           // always (may be [])
//	  timestamp:    <ISO ms string>,             // always
//	  authors:      [ <authorId|int|null>, ...],  // always (may be [])
//	  v2Authors:    [ <v2author>, ...],          // always ([], Node: [] is truthy)
//	  origin?:      <Origin.toRaw>,              // when present
//	  projectVersion?: <"major.minor">,          // when present
//	  v2DocVersions?: <V2DocVersions.toRaw>,     // when present
//	}
//
// Node Change.fromRaw(raw): raw falsy -> null, else mustFromRaw:
//   - operations: raw.operations.map(Operation.fromRaw)
//   - timestamp:  new Date(raw.timestamp) (assert.nonEmptyString)
//   - authors:    raw.authors ? raw.authors.map(a => a === 0 ? null : a) : undefined
//     -> constructor authors || []
//   - origin:     raw.origin ? Origin.fromRaw : null
//   - v2Authors:  raw.v2Authors (constructor v2Authors || [])
//   - projectVersion: raw.projectVersion (string "major.minor")
//   - v2DocVersions:  raw.v2DocVersions ? V2DocVersions.fromRaw : null
type Change struct {
	Operations     []*Operation
	Timestamp      time.Time
	Authors        []any
	V2Authors      []any
	Origin         *Origin
	ProjectVersion string
	V2DocVersions  *V2DocVersions
}

// NewChange (Node Change constructor: authors||[], v2Authors||[]).
func NewChange(operations []*Operation, timestamp time.Time, authors []any, origin *Origin, v2Authors []any, projectVersion string, v2dv *V2DocVersions) *Change {
	if v2Authors == nil {
		v2Authors = []any{}
	}
	if authors == nil {
		authors = []any{}
	}
	return &Change{
		Operations:     operations,
		Timestamp:      timestamp,
		Authors:        authors,
		Origin:         origin,
		V2Authors:      v2Authors,
		ProjectVersion: projectVersion,
		V2DocVersions:  v2dv,
	}
}

// ChangeFromRaw (Node Change.fromRaw). falsy raw -> nil.
func ChangeFromRaw(raw json.RawMessage) *Change {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	c, err := ChangeMustFromRaw(raw)
	if err != nil {
		panic(err)
	}
	return c
}

// ChangeMustFromRaw — typed read (Node Change.mustFromRaw).
func ChangeMustFromRaw(raw json.RawMessage) (*Change, error) {
	var probe struct {
		Operations     []json.RawMessage `json:"operations"`
		Timestamp      string            `json:"timestamp"`
		Authors        []json.RawMessage `json:"authors"`
		V2Authors      *json.RawMessage  `json:"v2Authors"`
		Origin         json.RawMessage   `json:"origin"`
		ProjectVersion *string           `json:"projectVersion"`
		V2DocVersions  json.RawMessage   `json:"v2DocVersions"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, &BadRawError{Msg: "bad raw in change.fromRaw: " + err.Error()}
	}
	if probe.Timestamp == "" {
		return nil, &BadRawError{Msg: "bad raw.timestamp (empty)"}
	}
	for _, oRaw := range probe.Operations {
		if len(oRaw) == 0 || string(oRaw) == "null" {
			return nil, &BadRawError{Msg: "bad operation in change.fromRaw"}
		}
	}
	t, err := time.Parse(time.RFC3339, probe.Timestamp)
	if err != nil {
		return nil, &BadRawError{Msg: "bad raw.timestamp: " + err.Error()}
	}
	ops := make([]*Operation, 0, len(probe.Operations))
	for _, oRaw := range probe.Operations {
		op, err := OperationFromRaw(oRaw)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	// authors: 0 -> null (Node "null represents an anonymous author").
	authors := []any{}
	for _, aRaw := range probe.Authors {
		var a any
		if err := json.Unmarshal(aRaw, &a); err != nil {
			return nil, &BadRawError{Msg: "bad author in change.fromRaw: " + err.Error()}
		}
		if _, isNull := a.(map[string]any); isNull { // won't happen (null unmarshals to nil)
			_ = isNull
		}
		// 0 -> null in Go is not detectable via type; check via raw scan.
		authors = append(authors, authorNorm(aRaw, a))
	}
	var v2authors []any
	if probe.V2Authors != nil && len(*probe.V2Authors) > 0 && string(*probe.V2Authors) != "null" {
		_ = json.Unmarshal(*probe.V2Authors, &v2authors)
	}
	c := &Change{
		Operations: ops,
		Timestamp:  t,
		Authors:    authors,
		V2Authors:  v2authors,
	}
	if len(probe.Origin) > 0 && string(probe.Origin) != "null" {
		c.Origin = OriginFromRaw(probe.Origin)
	}
	if probe.ProjectVersion != nil {
		c.ProjectVersion = *probe.ProjectVersion
	}
	if len(probe.V2DocVersions) > 0 && string(probe.V2DocVersions) != "null" {
		c.V2DocVersions = V2DocVersionsFromRaw(probe.V2DocVersions)
	}
	// Node Change constructor: v2Authors || [] (always present, truthy []).
	if c.V2Authors == nil {
		c.V2Authors = []any{}
	}
	return c, nil
}

// authorNorm — Node: author === 0 ? null : author.
func authorNorm(aRaw json.RawMessage, decoded any) any {
	if len(aRaw) == 1 && aRaw[0] == '0' {
		return nil
	}
	return decoded
}

// ToRaw (Node Change.toRaw).
func (c *Change) ToRaw() json.RawMessage {
	ops := make([]json.RawMessage, 0, len(c.Operations))
	for _, o := range c.Operations {
		ops = append(ops, o.ToRaw())
	}
	out := map[string]any{
		"operations": ops,
		"timestamp":  c.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z"),
		"authors":    c.Authors,
		"v2Authors":  c.V2Authors,
	}
	if c.Origin != nil {
		out["origin"] = json.RawMessage(c.Origin.ToRaw())
	}
	if c.ProjectVersion != "" {
		out["projectVersion"] = c.ProjectVersion
	}
	if c.V2DocVersions != nil {
		out["v2DocVersions"] = json.RawMessage(c.V2DocVersions.ToRaw())
	}
	b, _ := json.Marshal(out)
	return b
}

// ApplyTo (Node Change.applyTo) — strict iterative apply + snapshot metadata.
func (c *Change) ApplyTo(snap *Snapshot) error {
	if _, err := c.IterativelyApplyTo(snap, true); err != nil {
		return err
	}
	return nil
}

// IterativelyApplyTo (Node Change.iterativelyApplyTo) — apply each operation,
// then set projectVersion / v2DocVersions (merge) / timestamp. strict: non-
// recoverable errors returned; recoverable (EditMissingFileError /
// FileNotFoundError) returned only under strict.
func (c *Change) IterativelyApplyTo(snap *Snapshot, strict bool) ([]*Operation, error) {
	applied := make([]*Operation, 0, len(c.Operations))
	for _, o := range c.Operations {
		err := o.ApplyTo(snap)
		if err != nil {
			if !strict && !isRecoverableOpError(err) {
				return applied, err
			}
			if !strict && isRecoverableOpError(err) {
				// recoverable in non-strict: skip (do NOT record as applied).
			} else {
				return applied, err
			}
		}
		applied = append(applied, o)
	}
	if c.ProjectVersion != "" {
		snap.ProjectVersion = c.ProjectVersion
	}
	if c.V2DocVersions != nil {
		c.V2DocVersions.ApplyTo(snap)
	}
	snap.Timestamp = c.Timestamp
	return applied, nil
}

func isRecoverableOpError(err error) bool {
	_, ok1 := err.(*EditMissingFileError)
	_, ok2 := err.(*FileNotFoundError)
	return ok1 || ok2
}

// --- Persistence (ports Change.store / loadFiles / findBlobHashes) ---

// Store (Node Change.store): raw = toRaw(); raw.authors = _.uniq(raw.authors);
// raw.operations = pMap(ops, op.store(bs)); return raw.
func (c *Change) Store(bs BlobStoreI) (json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(c.ToRaw(), &raw); err != nil {
		return nil, &BadRawError{Msg: "change.store: bad toRaw: " + err.Error()}
	}
	authors := uniqAuthors(c.Authors)
	aB, err := json.Marshal(authors)
	if err != nil {
		return nil, &BadRawError{Msg: "change.store: bad authors: " + err.Error()}
	}
	raw["authors"] = aB
	storedOps := make([]json.RawMessage, 0, len(c.Operations))
	for _, op := range c.Operations {
		so, err := op.Store(bs)
		if err != nil {
			return nil, err
		}
		storedOps = append(storedOps, so)
	}
	ob, err := json.Marshal(storedOps)
	if err != nil {
		return nil, &BadRawError{Msg: "change.store: bad ops: " + err.Error()}
	}
	raw["operations"] = ob
	out, err := json.Marshal(raw)
	if err != nil {
		return nil, &BadRawError{Msg: "change.store: bad marshal: " + err.Error()}
	}
	return out, nil
}

// LoadFiles (Node Change.loadFiles): per-operation loadFiles.
func (c *Change) LoadFiles(kind string, bs BlobStoreI) error {
	for _, op := range c.Operations {
		if err := op.LoadFiles(kind, bs); err != nil {
			return err
		}
	}
	return nil
}

// FindBlobHashes (Node Change.findBlobHashes): per-operation findBlobHashes.
func (c *Change) FindBlobHashes(blobHashes *map[string]struct{}) {
	for _, op := range c.Operations {
		op.FindBlobHashes(blobHashes)
	}
}

// uniqAuthors — Node `_.uniq(raw.authors)`: preserve first-occurrence order,
// dedupe by parse-equivalence (authors are int, string, or null).
func uniqAuthors(authors []any) []any {
	seen := map[string]struct{}{}
	out := make([]any, 0, len(authors))
	for _, a := range authors {
		k, ok := authorKey(a)
		if !ok {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, a)
	}
	return out
}

// authorKey — stable JSON key for an author (Node _.uniq is ===-based, so
// distinct JS types are never deduped against each other; JSON scalar
// identity is exact). nil keys are not deduped (Node keeps them).
func authorKey(a any) (string, bool) {
	switch v := a.(type) {
	case nil:
		return "", false
	case float64:
		return "f:" + fmt.Sprintf("%v", v), true
	case int:
		return "i:" + fmt.Sprintf("%d", v), true
	case string:
		return "s:" + v, true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return "j:" + string(b), true
	}
}
