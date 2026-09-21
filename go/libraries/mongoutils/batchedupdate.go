package mongoutils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Doc is one document the batch walker sees (Node's `Document`; Go consumers
// decode into any map/struct shape the driver produces — batchedUpdate only
// reads `_id` via the map forms).
type Doc = map[string]any

const batchUpdateRunningError = "batchedUpdate is already running"

// batchedUpdate mirrors batchedUpdate(collection, query, update, projection,
// findOptions, batchedUpdateOptions) 1:1 — including the single-flight
// guard, the empty-collection early return, the per-batch range arithmetic,
// the trackProgress callbacks (with the default `console.warn` → stderr),
// the verbose id-log, and the `update`-as-function vs update-document
// dispatch.
func batchedUpdate(
	ctx context.Context,
	collection *mongo.Collection,
	query bson.M,
	update any,
	projection bson.M,
	findOpts *options.FindOptions,
	batchOpts BatchedUpdateOptions,
) (int, error) {
	batchStateMu.Lock()
	if BatchedUpdateRunning {
		batchStateMu.Unlock()
		return 0, fmt.Errorf("%s", batchUpdateRunningError)
	}
	BatchedUpdateRunning = true
	batchStateMu.Unlock()
	defer func() {
		batchStateMu.Lock()
		BatchedUpdateRunning = false
		batchStateMu.Unlock()
	}()

	idEdgePast, ok := awaitIDEdgePast(ctx, collection)
	if !ok {
		// Node console.warn: `The collection <name> appears to be empty.`
		fmt.Fprintln(os.Stderr, fmt.Sprintf("The collection %s appears to be empty.", collection.Name()))
		return 0, nil
	}
	setIDedgePast(idEdgePast)

	refreshGlobalOptionsForBatchedUpdate(batchOpts)

	trackProgress := batchOpts.TrackProgress
	if trackProgress == nil {
		// Node default: `trackProgress = async progress => console.warn(progress)`
		trackProgress = func(progress string) { fmt.Fprintln(os.Stderr, progress) }
	}

	batchStateMu.Lock()
	batchDescending := BatchDescending
	batchSize := BatchSize
	verbose := VerboseLogging
	batchRangeStart := BatchRangeStart
	batchRangeEnd := BatchRangeEnd
	batchMaxTimeSpanMs := BatchMaxTimeSpanMs
	batchStateMu.Unlock()

	if findOpts == nil {
		findOpts = options.Find()
	}
	// Node: findOptions.readPreference = READ_PREFERENCE_SECONDARY (per-find).
	// Go driver v1 carries the read preference on the Collection options, so
	// the faithful mapping is a collection view with that preference.
	collection = withReadPreference(collection, readPreferenceSecondary)

	if projection == nil {
		projection = bson.M{"_id": 1}
	}

	updated := 0
	start := batchRangeStart

	for start != batchRangeEnd {
		end := getNextEnd(start, batchMaxTimeSpanMs, batchDescending, batchRangeEnd)

		// Node mutates the caller's query object in place (query._id = ...);
		// Go value semantics: copy per batch — the observable filter (rebuilt
		// from iteration bounds) is identical either way.
		queryCopy := bson.M{}
		for k, v := range query {
			queryCopy[k] = v
		}
		if batchDescending {
			queryCopy["_id"] = bson.M{"$gt": end, "$lte": start}
		} else {
			queryCopy["_id"] = bson.M{"$gt": start, "$lte": end}
		}

		batch, err := getNextBatch(ctx, collection, queryCopy, start, end, projection, findOpts, batchSize)
		if err != nil {
			return updated, err
		}

		if len(batch) > 0 {
			last := batch[len(batch)-1]
			end, _ = objectIDOf(last)
			updated += len(batch)

			if verbose {
				ids := make([]any, len(batch))
				for i, b := range batch {
					id, _ := objectIDOf(b)
					ids[i] = id
				}
				idsJSON, _ := json.Marshal(ids)
				fmt.Fprintf(os.Stdout, "Running update on batch with ids %s\n", idsJSON) // Node console.log → stdout
			}

			trackProgress(fmt.Sprintf("Running update on batch ending %s", RenderObjectId(end)))

			if fn, isFn := update.(func([]Doc) error); isFn {
				if err := fn(batch); err != nil {
					return updated, err
				}
			} else {
				if err := performUpdate(ctx, collection, batch, update); err != nil {
					return updated, err
				}
			}
		}
		trackProgress(fmt.Sprintf("Completed batch ending %s", RenderObjectId(end)))
		start = end
	}
	return updated, nil
}

// the public 1:1 surface (Node batchedUpdate is exported under that name).
func BatchedUpdate(
	ctx context.Context,
	collection *mongo.Collection,
	query bson.M,
	update any,
	projection bson.M,
	findOpts *options.FindOptions,
	batchOpts BatchedUpdateOptions,
) (int, error) {
	return batchedUpdate(ctx, collection, query, update, projection, findOpts, batchOpts)
}

// batchedUpdateWithResultHandling mirrors the Node script-bootstrap (run the
// update; console.error the result and process.exit 0 on success / 1 on
// failure). A Go program embedding this bears the os.Exit — the same cost as
// the Node helper.
func BatchedUpdateWithResultHandling(
	ctx context.Context,
	collection *mongo.Collection,
	query bson.M,
	update any,
	projection bson.M,
	findOpts *options.FindOptions,
	batchOpts BatchedUpdateOptions,
) {
	processed, err := batchedUpdate(ctx, collection, query, update, projection, findOpts, batchOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "processed: %d\n", processed)
	os.Exit(0)
}

// getNextBatch mirrors getNextBatch — find + project + sort + limit over the
// (start, end] ObjectId window.
func getNextBatch(
	ctx context.Context,
	collection *mongo.Collection,
	query bson.M,
	start, end primitive.ObjectID,
	projection bson.M,
	findOpts *options.FindOptions,
	limit int,
) ([]Doc, error) {
	findOpts = findOpts.SetProjection(projection).SetSort(bson.D{{Key: "_id", Value: batchSortDir()}}).
		SetLimit(int64(limit))
	cursor, err := collection.Find(ctx, query, findOpts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var batch []Doc
	if err := cursor.All(ctx, &batch); err != nil {
		return nil, err
	}
	return batch, nil
}

func batchSortDir() int {
	batchStateMu.Lock()
	defer batchStateMu.Unlock()
	if BatchDescending {
		return -1
	}
	return 1
}

// performUpdate mirrors performUpdate — updateMany({_id: {$in: <ids>}}, update).
func performUpdate(ctx context.Context, collection *mongo.Collection, batch []Doc, update any) error {
	ids := make([]any, len(batch))
	for i, b := range batch {
		id, _ := objectIDOf(b)
		ids[i] = id
	}
	_, err := collection.UpdateMany(ctx, bson.M{"_id": bson.M{"$in": ids}}, update)
	return err
}

// objectIDOf reads a document's `_id` (map-shaped documents; the projection
// default guarantees its presence).
func objectIDOf(doc Doc) (primitive.ObjectID, bool) {
	id, ok := doc["_id"].(primitive.ObjectID)
	return id, ok
}

// getNextEnd mirrors getNextEnd(start).
func getNextEnd(start primitive.ObjectID, maxTimeSpanMs int64, descending bool, rangeEnd primitive.ObjectID) primitive.ObjectID {
	var end primitive.ObjectID
	if descending {
		end = objectIdFromMs(getMsFromObjectId(start) - maxTimeSpanMs)
		if getMsFromObjectId(end) <= getMsFromObjectId(rangeEnd) {
			end = rangeEnd
		}
	} else {
		end = objectIdFromMs(getMsFromObjectId(start) + maxTimeSpanMs)
		if getMsFromObjectId(end) >= getMsFromObjectId(rangeEnd) {
			end = rangeEnd
		}
	}
	return end
}

// getIdEdgePast mirrors getIdEdgePast — the first _id (ascending), pulled one
// second into the past so the first entry passes `first._id > ID_EDGE_PAST`;
// nil for an empty collection.
func awaitIDEdgePast(ctx context.Context, collection *mongo.Collection) (primitive.ObjectID, bool) {
	var first Doc
	err := collection.FindOne(ctx, bson.M{},
		options.FindOne().SetSort(bson.D{{Key: "_id", Value: 1}}).SetProjection(bson.M{"_id": 1}),
	).Decode(&first)
	if err != nil {
		return primitive.ObjectID{}, false
	}
	id, _ := objectIDOf(first)
	ms := getMsFromObjectId(id) - 1000
	if ms < 0 {
		ms = 0
	}
	return objectIdFromMs(ms), true
}

func setIDedgePast(id primitive.ObjectID) {
	batchStateMu.Lock()
	ideEdgePast = id
	hasIdeEdgePast = true
	batchStateMu.Unlock()
}

// modeByName maps the Node readPreference modes to the Go driver values:
// "secondary" → readpref.Secondary, "secondaryPreferred" →
// readpref.SecondaryPreferred.
func modeByName(mode string) *readpref.ReadPref {
	if mode == "secondary" {
		return readpref.Secondary()
	}
	return readpref.SecondaryPreferred()
}

// withReadPreference returns a view of the same collection whose operations
// use the given read preference (the Go v1 driver attaches read preferences
// to the collection, not to find options — the Node
// `findOptions.readPreference = ...` equivalent).
func withReadPreference(coll *mongo.Collection, mode string) *mongo.Collection {
	db := coll.Database()
	return db.Client().Database(db.Name()).Collection(coll.Name(),
		options.Collection().SetReadPreference(modeByName(mode)))
}
