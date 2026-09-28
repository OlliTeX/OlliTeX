// Package historyapimanager is the 1:1 port of
// services/project-history/app/js/HistoryApiManager.js (22 L).
//
// HA1 shouldUseProjectHistory(projectId): `historyId != null` → bool
// (error propagates from WebApiManager.getHistoryId).
package historyapimanager

import (
	"context"

	"ollitex/go/services/project-history/internal/webapimanager"
)

// Deps — the vendor WebApiManager import as a seam.
type Deps struct {
	WebApi *webapimanager.Deps
}

// ShouldUseProjectHistory — vendor shouldUseProjectHistory (HA1).
func (d *Deps) ShouldUseProjectHistory(ctx context.Context, projectID string) (bool, error) {
	if d == nil || d.WebApi == nil {
		return false, &webapimanager.RequestFailedError{Msg: "WebApiManager seam not wired"}
	}
	historyID, err := d.WebApi.GetHistoryId(ctx, projectID)
	if err != nil {
		return false, err
	}
	return historyID != "", nil
}
