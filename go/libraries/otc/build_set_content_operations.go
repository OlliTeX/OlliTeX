package otc

// build_set_content_operations.go — 1:1 port of `lib/build_set_content_operations.js`.
// Builds the operations that set a pathname's content (a setDoc-like upsert).

import (
	"context"
	"reflect"
	"time"

	oerror "ollitex/go/libraries/oerror"
)

// BuildSetContentTracking is the optional tracking argument
// (Node: `{userId, ts}`).
type BuildSetContentTracking struct {
	UserID string
	TS     time.Time
}

// BuildSetContentArgs are the arguments to BuildSetContentOperations
// (mirrors the destructured Node args object). Exactly one of Content and
// Blob must be present.
type BuildSetContentArgs struct {
	File      *File
	Pathname  string
	Content   *string        // new doc content (nil = not provided)
	Blob      *Blob          // new binary content (nil = not provided)
	Metadata  map[string]any // metadata to set; nil defaults to {}
	Tracking  *BuildSetContentTracking
	BlobStore BlobStore
}

// BuildSetContentResult mirrors Node's `{operations, status}`.
type BuildSetContentResult struct {
	Operations []Operation
	Status     string // "applied" | "noop"
}

// BuildSetContentOperations mirrors buildSetContentOperations — set a
// pathname's content (doc edit, or file replace/create), recording the new
// content (and, when requested, a whole-doc tracked insert) in the blob
// store.
func BuildSetContentOperations(ctx context.Context, args BuildSetContentArgs) (*BuildSetContentResult, error) {
	contentProvided := args.Content != nil
	blobProvided := args.Blob != nil
	if contentProvided == blobProvided {
		return nil, oerror.New(
			"buildSetContentOperations: exactly one of content and blob must be given",
			map[string]any{"pathname": args.Pathname})
	}

	if contentProvided {
		if args.File != nil {
			// Eager loading resolves both the editability of a file whose
			// data is a bare hash and the content to diff against.
			if _, err := args.File.Load(ctx, "eager", args.BlobStore); err != nil {
				return nil, err
			}
			if editable := args.File.IsEditable(); editable != nil && *editable {
				return buildEditOperations(args.File, args.Pathname, *args.Content, args.Metadata, args.Tracking)
			}
		}
		blob, err := args.BlobStore.PutString(ctx, *args.Content)
		if err != nil {
			return nil, err
		}
		args.Blob = blob
	}
	if args.Blob == nil {
		return nil, oerror.New(
			"buildSetContentOperations: no blob",
			map[string]any{"pathname": args.Pathname})
	}

	blob := args.Blob
	if args.File != nil {
		if h := args.File.GetHash(); h != nil && *h == blob.GetHash() {
			// LazyStringFileData yields no hash with buffered changes, so no
			// false-positive hash matches are possible.
			if metadataEqual(args.File.GetMetadata(), nullSafeMeta(args.Metadata)) {
				return &BuildSetContentResult{Operations: []Operation{}, Status: "noop"}, nil
			}
			op, err := NewSetFileMetadataOperation(args.Pathname, nullSafeMeta(args.Metadata))
			if err != nil {
				return nil, err
			}
			return &BuildSetContentResult{Operations: []Operation{op}, Status: "applied"}, nil
		}
	}

	var rangesBlob *Blob
	if contentProvided && len(*args.Content) > 0 && args.Tracking != nil {
		// Record the whole new doc as a tracked insert.
		ranges := map[string]any{
			"comments": []any{},
			"trackedChanges": []any{
				map[string]any{
					"range":    map[string]any{"pos": 0, "length": len(*args.Content)},
					"tracking": NewTrackingProps("insert", args.Tracking.UserID, args.Tracking.TS).ToRaw(),
				},
			},
		}
		rb, err := args.BlobStore.PutObject(ctx, ranges)
		if err != nil {
			return nil, err
		}
		rangesBlob = rb
	}

	newFile, err := FileCreateLazyFromBlobs(blob, rangesBlob, nullSafeMeta(args.Metadata))
	if err != nil {
		return nil, err
	}

	operations := []Operation{}
	if args.File != nil {
		// Operation.removeFile(pathname) is a MoveFileOperation with an empty
		// target (IsRemoveFile).
		operations = append(operations, &MoveFileOperation{Pathname: args.Pathname})
	}
	addOp, err := NewAddFileOperation(args.Pathname, newFile)
	if err != nil {
		return nil, err
	}
	operations = append(operations, addOp)
	return &BuildSetContentResult{Operations: operations, Status: "applied"}, nil
}

// buildEditOperations mirrors the private buildEditOperations — edits an
// eagerly-loaded editable file in place with a minimal diff, and (independently)
// sets its metadata when it differs.
func buildEditOperations(file *File, pathname string, content string, metadata map[string]any, tracking *BuildSetContentTracking) (*BuildSetContentResult, error) {
	fileData, ok := file.data.(*StringFileData)
	if !ok {
		return nil, &NotEditableError{}
	}
	var dtaOpts DiffOpts
	if tracking != nil {
		dtaOpts.Tracking = &DiffTracking{UserID: tracking.UserID, TS: tracking.TS}
	}
	textOp, err := DiffAsTextOperation(fileData, content, dtaOpts)
	if err != nil {
		return nil, err
	}

	operations := []Operation{}
	if !textOp.IsNoop() {
		operations = append(operations, &EditFileOperation{Pathname: pathname, Operation: NewTextEdit(textOp)})
	}
	if !metadataEqual(file.GetMetadata(), nullSafeMeta(metadata)) {
		op, err := NewSetFileMetadataOperation(pathname, nullSafeMeta(metadata))
		if err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}
	if len(operations) == 0 {
		return &BuildSetContentResult{Operations: operations, Status: "noop"}, nil
	}
	return &BuildSetContentResult{Operations: operations, Status: "applied"}, nil
}

// nullSafeMeta mirrors Node's `metadata = {}` default.
func nullSafeMeta(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// metadataEqual mirrors Node's `_.isEqual(file.getMetadata(), metadata)`
// (deep structural equality on the JSON-like metadata map).
func metadataEqual(a, b map[string]any) bool {
	return reflect.DeepEqual(a, b)
}
