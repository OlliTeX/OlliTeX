// Package contentcacheworker ports services/clsi/app/js/ContentCacheWorker.js
// (4L).
//
// Node:
//
//	import workerpool from 'workerpool'
//	import ContentCacheManager from './ContentCacheManager.js'
//	workerpool.worker(ContentCacheManager.promises)
//
// The worker exposes `ContentCacheManager.promises` (async versions of
// `update` and `updateSameEventLoop`) to its workerpool host. In Go the
// equivalent is a thin package that re-exposes the same entry point, so the
// "worker" can call into `contentcachemanager.Update` without re-implementation.
package contentcacheworker

import (
	"clsi/contentcachemanager"
)

// Worker mirrors `ContentCacheManager.promises` (the workerpool-registered
// method set). It provides the one public promise method called from the
// workerpool host.
type Worker struct{}

// Update forwards to ContentCacheManager.Update (promises.update).
func (w Worker) Update(a contentcachemanager.UpdateArgs) (res *contentcachemanager.UpdateResult, err error) {
	return contentcachemanager.Update(a)
}
