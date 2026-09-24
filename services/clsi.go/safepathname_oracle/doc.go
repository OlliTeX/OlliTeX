package safepathname_oracle

// Package safepathname_oracle is a doc-only file. The CLSI-side acceptance
// test (safepathname_oracle_test.go) validates the ollitex/go/libraries/otc
// safe_pathname implementation against the 80,782-row Node-generated fuzz
// fixture in testdata/spfuzz2.json.
//
// See services/clsi.go/safepathname_oracle/ for why this exists: otc is
// shared between the Node CLSI port and the rest of ollitex, and CLSI
// consumers need a single acceptance contract.
