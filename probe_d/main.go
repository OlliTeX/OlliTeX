package main

import (
	"context"
	"fmt"
	"time"

	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"ollitex/go/services/collab"
	"ollitex/go/services/web/features/history"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:27017").SetDirect(true))
	if err != nil {
		panic(err)
	}
	db := client.Database("ollitex")
	pid := "6ac54f1acb0b784fbc32d3be"

	st, err := collab.NewMongoStore(ctx, db)
	if err != nil {
		panic(err)
	}
	var vlog collab.Log
	if vl, verr := collab.NewMongoVersionLog(ctx, db); verr == nil {
		vlog = vl
	}
	seek := history.MongoDocSeeker(db)
	rooms, rerr := history.ProjectDocRooms(ctx, st, vlog, seek, pid)
	fmt.Printf("projectDocRooms: n=%d err=%v\n", len(rooms), rerr)
	for i := range rooms {
		fmt.Printf("  room=%q root=%v path=%q\n", rooms[i].Room, rooms[i].IsRoot, rooms[i].Pathname)
		lvs, _ := st.ListVersions(ctx, rooms[i].Room)
		fmt.Printf("    versions=%d", len(lvs))
		if len(lvs) > 0 {
			fmt.Printf(" latest=%d oldest=%d", lvs[0].Version, lvs[len(lvs)-1].Version)
		}
		fmt.Println()
	}
	yops, _ := history.NewMongoYopLog(ctx, db)
	ops, oerr := yops.List(ctx, pid)
	fmt.Printf("yops: n=%d err=%v\n", len(ops), oerr)
	rootPath := ""
	if r2, r2err := st.Load(ctx, pid); r2err == nil {
		fmt.Printf("root room Load version=%d\n", r2.Version)
	}
	_ = rootPath
	_ = persistence.Version(1)

	// Reproduce the multi-room filetree branch for from=502, to=503.
	from, to := 502, 503
	feed := history.BuildUnifiedFeedMulti(rooms, rootPath, ops)
	fmt.Printf("UNIFIED feed len=%d\n", len(feed))
	ok := from >= 1 && len(feed) >= to-1
	fmt.Printf("gate from>=1 && len(feed)>=to-1: %v (len=%d to-1=%d)\n", ok, len(feed), to-1)
	if len(feed) > 0 {
		last := feed[len(feed)-1]
		fmt.Printf("last item: UnifiedV=%d src=%d v=%d kind=%d path=%s\n", last.UnifiedV, last.Source, last.V, last.Kind, last.Pathname)
	}
	// per-room mapping for the failing range
	for i := range rooms {
		bv, bf := history.FileAtTimeline(feed, from-1, rooms[i].Room)
		av, af := history.FileAtTimeline(feed, to-1, rooms[i].Room)
		var berr, aerr error
		var bc, ac string
		if bf {
			bc, berr = collab.TextAt(ctx, st, rooms[i].Room, persistence.Version(bv))
		}
		if af {
			ac, aerr = collab.TextAt(ctx, st, rooms[i].Room, persistence.Version(av))
		}
		fmt.Printf("room %q: bv=%d(found=%v,berr=%v) av=%d(found=%v,aerr=%v) sameText=%v\n",
			rooms[i].Room, bv, bf, berr, av, af, aerr, berr == nil && aerr == nil && bc == ac)
	}
}
