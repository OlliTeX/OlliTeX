package gsync

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Regression pin: gsProjectState must decode a document of the exact shape
// gsSaveState writes (lastSyncCommit etc. must not decode to zero values).
func TestStateDecodeFields(t *testing.T) {
	doc := bson.D{
		{Key: "projectId", Value: bson.NewObjectID()},
		{Key: "repoFullName", Value: "oltest/oltest-repo"},
		{Key: "mergeStatus", Value: "clean"},
		{Key: "lastSyncCommit", Value: "b7d981a7f6d856c602e89d858920dd536cae15b4"},
		{Key: "defaultBranchName", Value: "main"},
		{Key: "lastSyncVersion", Value: int64(1)},
		{Key: "syncProvider", Value: "forgejo"},
		{Key: "syncServerUrl", Value: "http://172.18.0.1:3000"},
		{Key: "syncUsername", Value: "oltest"},
		{Key: "ownerId", Value: "u1"},
	}
	raw, err := bson.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var st gsProjectState
	if err := bson.Unmarshal(raw, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	t.Logf("decoded: repo=%q status=%q last='%s' branch=%q ver=%d prov=%q srv=%q user=%q",
		st.RepoFullName, st.MergeStatus, st.LastSyncCommit, st.DefaultBranchName, st.LastSyncVersion,
		st.SyncProvider, st.SyncServerURL, st.SyncUsername)
	if st.LastSyncCommit == "" {
		t.Fatal("BUG REPRODUCED: lastSyncCommit did not decode (zero value)")
	}
}
