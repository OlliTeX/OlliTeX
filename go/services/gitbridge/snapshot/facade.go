// facade.go — ports bridge/snapshot/SnapshotApiFacade + the SnapshotApi
// interface (satisfied by *NetSnapshotApi via method promotion).
package snapshot

import (
	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/giterrors"
	"ollitex/go/services/gitbridge/util"
)

// ---------------------------------------------------------------------------
// SnapshotApi (interface, ports bridge/snapshot/SnapshotApi).
//
// Nil token == Java Optional.empty() (no oauth2). The token is threaded for
// call-shape parity; the concrete Net impl does not attach it to the wire
// (Java carries it as an interceptor that only fires on real OAuth-enabled
// endpoints; the mock/bridge flow never depends on it). See note in
// NetSnapshotApi below for the wire Authorization behavior.
// ---------------------------------------------------------------------------

// SnapshotApi ports the Java SnapshotApi interface.
type SnapshotApi interface {
	GetDoc(token *data.Oauth2, projectName string) (*GetDocResult, error)
	GetForVersion(token *data.Oauth2, projectName string, versionID int) (*SnapshotData, error)
	GetSavedVers(token *data.Oauth2, projectName string) ([]SnapshotInfo, error)
	Push(token *data.Oauth2, projectName string, body []byte) (*PushResult, error)
}

// ---------------------------------------------------------------------------
// Facade (ports bridge/snapshot/SnapshotApiFacade).
//
// Wraps a SnapshotApi and adds:
//   - ProjectExists / GetDoc: the "project not found" → not-present mapping.
//   - GetSnapshots: the merged (saved versions + synthetic latest) list,
//     ascending by versionId, each with its SnapshotData.
//   - Push: passthrough.
// ---------------------------------------------------------------------------

// Facade ports SnapshotApiFacade.
type Facade struct{ api SnapshotApi }

// NewFacade ports the SnapshotApiFacade(SnapshotApi) constructor.
func NewFacade(api SnapshotApi) *Facade { return &Facade{api: api} }

// ProjectExists ports SnapshotApiFacade.projectExists.
func (f *Facade) ProjectExists(token *data.Oauth2, projectName string) (bool, error) {
	doc, err := f.api.GetDoc(token, projectName)
	if err != nil {
		if isInvalidProject(err) {
			return false, nil
		}
		return false, err
	}
	// Java: getVersionID() throws InvalidProjectException on the 404 marker,
	// which the facade catches and reports as "does not exist".
	if _, err := doc.VersionIDErr(); err != nil {
		return false, nil
	}
	return true, nil
}

// GetDoc ports SnapshotApiFacade.getDoc (nil = not present).
func (f *Facade) GetDoc(token *data.Oauth2, projectName string) (*GetDocResult, error) {
	doc, err := f.api.GetDoc(token, projectName)
	if err != nil {
		if isInvalidProject(err) {
			return nil, nil
		}
		return nil, err
	}
	if _, err := doc.VersionIDErr(); err != nil {
		return nil, nil
	}
	return doc, nil
}

// GetSnapshots ports SnapshotApiFacade.getSnapshots.
func (f *Facade) GetSnapshots(token *data.Oauth2, projectName string, afterVersionID int) ([]*data.Snapshot, error) {
	infos, err := f.snapshotInfosAfter(token, projectName, afterVersionID)
	if err != nil {
		return nil, err
	}
	datas := make([]*SnapshotData, 0, len(infos))
	for _, info := range infos {
		sd, err := f.api.GetForVersion(token, projectName, info.VersionID)
		if err != nil {
			return nil, err
		}
		datas = append(datas, sd)
	}
	return combineSnapshotInfosDatas(infos, datas), nil
}

// Push ports SnapshotApiFacade.push (delegate).
func (f *Facade) Push(token *data.Oauth2, projectName string, body []byte) (*PushResult, error) {
	return f.api.Push(token, projectName, body)
}

// snapshotInfosAfter ports getSnapshotInfosAfterVersion.
//
// Java fires getDoc + getSavedVers and joins them. Go does the two requests
// (the observable result is identical: both are needed, neither depends on
// the other).
//
// Semantics (Java 1:1):
//
//	latest := getDoc().getVersionID()
//	if latest > version || (latest == 0 && version == 0)   // PR #50 edge
//	    for v in getSavedVers(): if v.id > version: add v
//	    add synthetic SnapshotInfo(latest, createdAt, name, email)
//	return sorted by versionId (TreeSet → ascending)
func (f *Facade) snapshotInfosAfter(token *data.Oauth2, projectName string, version int) ([]SnapshotInfo, error) {
	latestDoc, err := f.api.GetDoc(token, projectName)
	if err != nil {
		if isInvalidProject(err) {
			return nil, nil
		}
		return nil, err
	}
	latest, err := latestDoc.VersionIDErr()
	if err != nil {
		if isInvalidProject(err) {
			return nil, nil
		}
		return nil, err
	}
	vers, err := f.api.GetSavedVers(token, projectName)
	if err != nil {
		// getSavedVers is only reached when getDoc succeeded; a missing
		// project here means nothing newer to apply.
		if isInvalidProject(err) {
			vers = nil
		} else {
			return nil, err
		}
	}
	m := map[int]SnapshotInfo{}
	for _, v := range vers {
		if v.VersionID > version {
			m[v.VersionID] = v // TreeSet: equals/versionId unique
		}
	}
	if latest > version || (latest == 0 && version == 0) {
		m[latest] = SnapshotInfo{
			VersionID: latest,
			Comment:   "Update on " + util.GetServiceName() + ".",
			User:      User{Name: latestDoc.Name, Email: latestDoc.Email},
			CreatedAt: latestDoc.CreatedAt,
		}
	}
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sortIntsAsc(ids)
	out := make([]SnapshotInfo, 0, len(ids))
	for _, id := range ids {
		out = append(out, m[id])
	}
	return out, nil
}

// combineSnapshotInfosDatas ports combine (infos[i] + datas[i] → Snapshot).
func combineSnapshotInfosDatas(infos []SnapshotInfo, datas []*SnapshotData) []*data.Snapshot {
	out := make([]*data.Snapshot, 0, len(infos))
	for i, info := range infos {
		if i >= len(datas) {
			break // (Java: iterator exhaustion → zip; safe bound here)
		}
		sd := datas[i]
		out = append(out, &data.Snapshot{
			VersionID: info.VersionID,
			Comment:   info.Comment,
			User:      data.User{Name: info.User.Name, Email: info.User.Email},
			CreatedAt: data.ParseSnapshotCreated(info.CreatedAt),
			Srcs:      toDomainFiles(sd.Srcs),
			Atts:      toDomainAtts(sd.Atts),
		})
	}
	return out
}

func toDomainFiles(in []SnapshotFile) []data.SnapshotFile {
	out := make([]data.SnapshotFile, 0, len(in))
	for _, f := range in {
		out = append(out, data.SnapshotFile{Path: f.Path, Contents: f.Contents})
	}
	return out
}

func toDomainAtts(in []SnapshotAttachment) []data.SnapshotAttachment {
	out := make([]data.SnapshotAttachment, 0, len(in))
	for _, a := range in {
		out = append(out, data.SnapshotAttachment{URL: a.URL, Path: a.Path})
	}
	return out
}

// isInvalidProject reports whether err is a "project does not exist" error
// (404 MissingRepositoryException or the GetDoc 404 marker).
func isInvalidProject(err error) bool {
	if err == nil {
		return false
	}
	switch err.(type) {
	case *giterrors.MissingRepositoryException:
		return true
	case *giterrors.GetDocInvalidProjectException:
		return true
	}
	return false
}

func sortIntsAsc(xs []int) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}
