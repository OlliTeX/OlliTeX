package otc

import (
	"time"
)

// Mirrors libraries/overleaf-editor-core/lib/label.js.

// Label is a user-configurable label that can be attached to a specific
// change. Labels are not versioned, and they are not stored alongside the
// Changes in Chunks. They are instead intended to provide external markers into
// the history of the project.
type Label struct {
	Text      string
	AuthorID  *int64 // nil == nullish (Node allows null for historical data)
	Timestamp time.Time
	Version   int64
}

// RawLabel mirrors the RawLabel wire shape consumed/produced by fromRaw/toRaw.
type RawLabel struct {
	Text      string
	AuthorID  *int64
	Timestamp string // ISO-8601
	Version   int64
}

// NewLabel mirrors the Label constructor with its check-types assertions (text
// a string, authorId a maybe-integer, timestamp a Date, version an integer).
func NewLabel(text string, authorID *int64, timestamp time.Time, version int64) *Label {
	return &Label{Text: text, AuthorID: authorID, Timestamp: timestamp, Version: version}
}

// LabelFromRaw mirrors Label.fromRaw (new Date(raw.timestamp)).
func LabelFromRaw(raw *RawLabel) *Label {
	ts, err := time.Parse(time.RFC3339Nano, raw.Timestamp)
	if err != nil {
		// Node: new Date(invalid) -> Invalid Date; we surface a parse error as a
		// panic (the oracle only drives fromRaw with valid timestamps).
		panic(typeErrorf("Label: bad timestamp %q", raw.Timestamp))
	}
	return &Label{Text: raw.Text, AuthorID: raw.AuthorID, Timestamp: ts, Version: raw.Version}
}

// ToRaw mirrors Label.toRaw (timestamp rendered as an ISO-8601 string).
func (l *Label) ToRaw() *RawLabel {
	return &RawLabel{
		Text:      l.Text,
		AuthorID:  l.AuthorID,
		Timestamp: l.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z"),
		Version:   l.Version,
	}
}

// GetText mirrors Label#getText.
func (l *Label) GetText() string { return l.Text }

// GetAuthorID mirrors Label#getAuthorId (nil == nullish).
func (l *Label) GetAuthorID() *int64 { return l.AuthorID }

// GetTimestamp mirrors Label#getTimestamp.
func (l *Label) GetTimestamp() time.Time { return l.Timestamp }

// GetVersion mirrors Label#getVersion.
func (l *Label) GetVersion() int64 { return l.Version }
