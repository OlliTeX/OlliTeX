package history

// zz_live_probe_d_test.go — LIVE probe for the D item (history 500).
// Skips when Mongo is unreachable. Run with:
//   go test ./go/services/web/features/history/ -run TestZZLiveDProbe -v

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"ollitex/go/services/collab"
)

func TestZZLiveDProbe(t *testing.T) {
	if _, err := net.DialTimeout("tcp", "127.0.0.1:27017", 2*time.Second); err != nil {
		t.Skip("mongo unreachable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:27017").SetDirect(true))
	if err != nil {
		t.Skip("mongo connect: ", err.Error())
	}
	db := client.Database("ollitex")
	pid := "6ac54f1acb0b784fbc32d3be"

	st, err := collab.NewMongoStore(ctx, db)
	if err != nil {
		t.Skip("store: ", err.Error())
	}
	var vlog collab.Log
	if vl, verr := collab.NewMongoVersionLog(ctx, db); verr == nil {
		vlog = vl
	}
	seek := MongoDocSeeker(db)
	rooms, rerr := projectDocRooms(ctx, st, vlog, seek, pid)
	fmt.Printf("rooms: n=%d err=%v\n", len(rooms), rerr)
	for i := range rooms {
		lvs, _ := st.ListVersions(ctx, rooms[i].Room)
		fmt.Printf("  room=%q root=%v path=%q versions=%d\n", rooms[i].Room, rooms[i].IsRoot, rooms[i].Pathname, len(lvs))
	}
	ops, oerr := NewMongoYopLog(ctx, db)
	yops, _ := ops.List(ctx, pid)
	fmt.Printf("yops: n=%d err=%v\n", len(yops), oerr)
	rootPath := rootDocPathname(ctx, seek, pid)
	fmt.Printf("rootPath=%q\n", rootPath)

	for _, rng := range [][2]int{{501, 502}, {502, 503}} {
		from, to := rng[0], rng[1]
		if len(rooms) > 1 {
			feed := buildUnifiedFeedMulti(rooms, rootPath, yops)
			fmt.Printf("range %d-%d: MULTI feed=%d gate=%v\n", from, to, len(feed), from >= 1 && len(feed) >= to-1)
			continue
		}
		// single-room S3b path
		lvs, _ := st.ListVersions(ctx, pid)
		vmetas := map[uint64]collab.VersionMeta{}
		if len(lvs) > 0 {
			var maxV uint64
			for _, lv := range lvs {
				if uint64(lv.Version) > maxV {
					maxV = uint64(lv.Version)
				}
			}
			if metas, merr := vlog.Range(ctx, pid, 0, maxV); merr == nil {
				for _, m := range metas {
					vmetas[m.V] = m
				}
			}
		}
		feed := buildUnifiedFeed(lvs, vmetas, yops, rootPath)
		fmt.Printf("range %d-%d: SINGLE feed=%d (lvs=%d yops=%d)\n", from, to, len(feed), len(lvs), len(yops))
		b, eB := textAt(ctx, st, pid, from-1)
		a, eA := textAt(ctx, st, pid, to-1)
		fmt.Printf("  textAt(from-1=%d): err=%v len=%d | textAt(to-1=%d): err=%v len=%d\n", from-1, eB, len(b), to-1, eA, len(a))
		if eB == nil && eA == nil {
			fmt.Printf("  -> would compose (edited=%v)\n", b != a)
		} else {
			fmt.Printf("  -> BAILS to legacy proxy (eB=%v eA=%v)\n", eB, eA)
		}
	}
}
