package projectlist

// d5dd23dd S3c — record tree mutations in the Yjs-plane tree-op log (yops),
// so /updates (S3a) and /filetree/diff (S3b) can compose them. Best-effort:
// a recording failure NEVER breaks the mutation (the log is a side channel;
// the entity mutation itself is the source of truth).

import (
	"context"
	"strings"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/history"
)

func yopsAppend(a *core.App, ctx context.Context, pidHex string, kind history.YopKind, path, newPath, uid string) {
	if a == nil || a.Mongo == nil || pidHex == "" {
		return
	}
	if db, err := a.Mongo.DB(ctx); err == nil {
		if yl, yerr := history.NewMongoYopLog(ctx, db); yerr == nil {
			yl.Append(ctx, history.YopMeta{
				Room:     strings.ToLower(pidHex),
				Kind:     kind,
				Pathname: path,
				NewPath:  newPath,
				UID:      uid,
				At:       time.Now().UTC(),
			})
		}
	}
}
