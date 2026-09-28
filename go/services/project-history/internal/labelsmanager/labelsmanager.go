// Package labelsmanager is the 1:1 port of
// services/project-history/app/js/LabelsManager.js (195 L).
//
// Faithful semantics:
//
//	L1 cloneLabels: find the source labels (projection {_id: 0, project_id: 0};
//	   ABSENT → callback() with NO labels — no insert); present → insertMany
//	   (each label spread + project_id = target ObjectId) — errors tagged.
//	L2 getLabels: find by project_id → _formatLabel each
//	   {id, comment, version, user_id, created_at}.
//	L3 createLabel(projectId, userId, version, comment, createdAt,
//	   shouldValidateExists):
//	   - shouldValidateExists === false → skip validation; else
//	     _validateChunkExistsForVersion: UpdatesProcessor.processUpdatesForProject
//	     → WebApiManager.getHistoryId → HistoryStoreManager.getChunkAtVersion
//	     (any step error → that error).
//	   - createdAt = createdAt != null ? new Date(createdAt) : new Date().
//	   - label {project_id, comment, version, created_at}; user_id ONLY when
//	     userId is truthy; after insert → label._id = confirmation.insertedId
//	     → callback(null, _formatLabel(label)).
//	L4 deleteLabelForUser: deleteOne {_id, project_id, user_id}.
//	L5 deleteLabel: deleteOne {_id, project_id}.
//	L6 transferLabels: updateMany {user_id: from} → {$set: {user_id: to}}.
//	L7 _toObjectId: id falsy → undefined (Go: empty string → "" and the
//	   OPERATION SHAPES MUST OMIT IT… vendor: ObjectId("") throws at `new
//	   ObjectId(id)` only when truthy — falsy args stay undefined and the
//	   caller uses them in filters — the port mirrors by passing a sentinel
//	   and OMITTING the filter key).
package labelsmanager

import (
	"context"
)

// LabelsCollection — the vendor `db.projectHistoryLabels` surface.
type LabelsCollection interface {
	Find(ctx context.Context, filter map[string]any, projection map[string]any) ([]map[string]any, error)
	InsertMany(ctx context.Context, docs []map[string]any) error
	InsertOne(ctx context.Context, doc map[string]any) (any, error) // returns insertedId
	DeleteOne(ctx context.Context, filter map[string]any) error
	UpdateMany(ctx context.Context, filter map[string]any, set map[string]any) error
}

// Deps — the vendor imports as seams.
type Deps struct {
	Labels LabelsCollection
	// ProcessUpdates — UpdatesProcessor.processUpdatesForProject (L3).
	ProcessUpdates func(ctx context.Context, projectID string) error
	// GetHistoryId — WebApiManager.getHistoryId (L3).
	GetHistoryId func(ctx context.Context, projectID string) (string, error)
	// GetChunkAtVersion — HistoryStoreManager.getChunkAtVersion (L3).
	GetChunkAtVersion func(ctx context.Context, projectID, historyID string, version int) (map[string]any, error)
	// Now — the `new Date()` clock (L3 createdAt default).
	Now func() any
}

// --- _formatLabel (shared) ---------------------------------------------------

func formatLabel(label map[string]any) map[string]any {
	return map[string]any{
		"id":         label["_id"],
		"comment":    label["comment"],
		"version":    label["version"],
		"user_id":    label["user_id"],
		"created_at": label["created_at"],
	}
}

// --- exports ---------------------------------------------------------------

// CloneLabels — vendor cloneLabels (L1).
func (d *Deps) CloneLabels(ctx context.Context, sourceProjectID, targetProjectID string) error {
	labels, err := d.Labels.Find(ctx, map[string]any{"project_id": sourceProjectID}, map[string]any{"_id": 0, "project_id": 0})
	if err != nil {
		return err
	}
	if len(labels) == 0 {
		return nil // vendor: callback() with no insert
	}
	docs := make([]map[string]any, 0, len(labels))
	for _, label := range labels {
		m := map[string]any{}
		for k, v := range label {
			m[k] = v
		}
		m["project_id"] = targetProjectID
		docs = append(docs, m)
	}
	return d.Labels.InsertMany(ctx, docs)
}

// GetLabels — vendor getLabels (L2).
func (d *Deps) GetLabels(ctx context.Context, projectID string) ([]map[string]any, error) {
	labels, err := d.Labels.Find(ctx, map[string]any{"project_id": projectID}, nil)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(labels))
	for _, l := range labels {
		out = append(out, formatLabel(l))
	}
	return out, nil
}

// CreateLabel — vendor createLabel (L3).
func (d *Deps) CreateLabel(ctx context.Context, projectID, userID string, version int, comment string, createdAt any, shouldValidateExists bool) (map[string]any, error) {
	d = d.withNow()
	// validateVersionExists
	if shouldValidateExists {
		if e := d.validateChunkExists(ctx, projectID, version); e != nil {
			return nil, e
		}
	}
	// createdAt = createdAt != null ? new Date(createdAt) : new Date()
	createdAtValue := createdAt
	if createdAtValue == nil {
		createdAtValue = d.Now()
	}
	label := map[string]any{
		"project_id": projectID,
		"comment":    comment,
		"version":    version,
		"created_at": createdAtValue,
	}
	if userID != "" {
		label["user_id"] = userID
	}
	insertedID, err := d.Labels.InsertOne(ctx, label)
	if err != nil {
		return nil, err
	}
	label["_id"] = insertedID
	return formatLabel(label), nil
}

// DeleteLabelForUser — vendor deleteLabelForUser (L4).
func (d *Deps) DeleteLabelForUser(ctx context.Context, projectID, userID, labelID string) error {
	filter := map[string]any{
		"_id":        labelID,
		"project_id": projectID,
	}
	if userID != "" { // L7: falsy userId → the key is ABSENT from the filter
		filter["user_id"] = userID
	}
	return d.Labels.DeleteOne(ctx, filter)
}

// DeleteLabel — vendor deleteLabel (L5).
func (d *Deps) DeleteLabel(ctx context.Context, projectID, labelID string) error {
	return d.Labels.DeleteOne(ctx, map[string]any{
		"_id":        labelID,
		"project_id": projectID,
	})
}

// TransferLabels — vendor transferLabels (L6).
func (d *Deps) TransferLabels(ctx context.Context, fromUserID, toUserID string) error {
	return d.Labels.UpdateMany(ctx, map[string]any{"user_id": fromUserID}, map[string]any{"user_id": toUserID})
}

// --- internals ---------------------------------------------------------------

func (d *Deps) withNow() *Deps {
	if d == nil {
		d = &Deps{}
	}
	if d.Now == nil {
		d.Now = func() any { return "now" } // the D phase wires a *time.Time
	}
	return d
}

// validateChunkExistsForVersion — vendor _validateChunkExistsForVersion (L3).
func (d *Deps) validateChunkExists(ctx context.Context, projectID string, version int) error {
	if d.ProcessUpdates != nil {
		if e := d.ProcessUpdates(ctx, projectID); e != nil {
			return e
		}
	}
	historyID := ""
	if d.GetHistoryId != nil {
		hid, e := d.GetHistoryId(ctx, projectID)
		if e != nil {
			return e
		}
		historyID = hid
	}
	if d.GetChunkAtVersion != nil {
		if _, e := d.GetChunkAtVersion(ctx, projectID, historyID, version); e != nil {
			return e
		}
	}
	return nil
}
