// Command seaweed-migrate converts files between the flat-FS persistor
// layout and the SeaweedFS (S3) layout for the two services that keep
// persisted blobs:
//
//   - docstore archives:   flat <projectID>_<docID>  ↔  S3 <projectID>/<docID>
//   - filestore blobs:     flat <projectID>_<path…>  ↔  S3 <projectID>/<path…>
//
// The S3 keys are exactly what the Node S3Persistor and the Go s3 stores
// write (keys stored verbatim, slashes intact), so an S3 side produced here
// is directly readable by either runtime; the flat side is exactly what the
// FSPersistor and the Go fs stores expect.
//
// fs → s3 note: the flat layout flattens every '/' to '_', so reconstructing
// the original S3 key from a bare filename is exact for docstore archives
// (both halves are 24-hex ids) and heuristic for filestore path segments
// that themselves contain '_' (the tool reports how many files were split
// that way; for a lossless filestore migration pass --keys, a TSV of
// <flat_name> <true_key> rows — e.g. from the `files` Mongo collection).
//
// s3 → fs is always lossless (the S3 key is authoritative).
//
// Usage:
//
//	health       --endpoint http://127.0.0.1:8333
//	list         --endpoint URL --bucket B [--prefix P]
//	to-seaweed   --endpoint URL --bucket B --dir FLAT_DIR
//	                   [--project 24HEX] [--keys FILE] [--dry-run]
//	from-seaweed --endpoint URL --bucket B --dir OUT_DIR [--prefix P] [--verify]
package main

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"ollitex/go/s3x"
)

var (
	pidDidRe = regexp.MustCompile(`^([0-9a-f]{24})_([0-9a-f]{24})$`)
	fsidRe   = regexp.MustCompile(`^[0-9a-f]{24}`)
)

// keyFromFlatName reconstructs the S3 key from a flat FS filename:
//   - docstore archive: <pid>_<did> → <pid>/<did> (exact)
//   - filestore:        <pid>_<a>_<b>… → <pid>/<a>/<b>… (heuristic when a
//     path segment contained '_'; exact when it did not)
//
// It returns (key, exact, err).
func keyFromFlatName(name string) (string, bool, error) {
	if m := pidDidRe.FindStringSubmatch(name); m != nil {
		return m[1] + "/" + m[2], true, nil
	}
	if !fsidRe.MatchString(name) {
		return "", false, fmt.Errorf("filename %q has no 24-hex project prefix", name)
	}
	pid := name[:24]
	if len(name) < 25 {
		return "", false, fmt.Errorf("filename %q is a bare project id", name)
	}
	rest := name[25:] // drop the flattening '_'
	segments := strings.Split(rest, "_")
	key := pid
	ok := true
	for _, seg := range segments {
		if seg == "" {
			ok = false
		}
		key += "/" + seg
	}
	return key, ok, nil
}

// flatFromKey is the inverse (always exact) for s3 → fs.
func flatFromKey(key string) string {
	return strings.ReplaceAll(key, "/", "_")
}

func md5File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type opts struct {
	endpoint string
	bucket   string
	dir      string
	prefix   string
	project  string
	keysFile string
	dryRun   bool
	verify   bool
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "seaweed-migrate: "+format+"\n", a...)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: seaweed-migrate <health|list|to-seaweed|from-seaweed> --flags")
	}
	cmd := os.Args[1]
	o := parseFlags(os.Args[2:])
	c := s3x.New(
		o.endpoint,
		envOr("AWS_ACCESS_KEY_ID", os.Getenv("AWS_KEY")),
		envOr("AWS_SECRET_ACCESS_KEY", os.Getenv("AWS_SECRET")),
	)

	switch cmd {
	case "health":
		// GET / (ListAllMyBuckets) — a live gateway answers 2xx; SeaweedFS
		// 405s a HEAD on the root path, so use GET, not HeadBucket.
		resp, err := c.Ping()
		if err != nil {
			fatalf("gateway unreachable at %s: %v", o.endpoint, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			fatalf("gateway answered %d at %s", resp.StatusCode, o.endpoint)
		}
		fmt.Printf("seaweed gateway OK (%s)\n", o.endpoint)
	case "list":
		listCmd(c, o)
	case "to-seaweed":
		toSeaweed(c, o)
	case "from-seaweed":
		fromSeaweed(c, o)
	default:
		fatalf("unknown command %q (want health|list|to-seaweed|from-seaweed)", cmd)
	}
}

func listCmd(c *s3x.Client, o opts) {
	objs, err := c.ListObjects(o.bucket, o.prefix)
	if err != nil {
		fatalf("list: %v", err)
	}
	var keys []string
	sizes := map[string]int64{}
	for _, ob := range objs {
		keys = append(keys, ob.Key)
		sizes[ob.Key] = ob.Size
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s\t%d\n", k, sizes[k])
	}
	fmt.Printf("// %d object(s)\n", len(keys))
}

func toSeaweed(c *s3x.Client, o opts) {
	if o.bucket == "" {
		fatalf("--bucket is required")
	}
	if o.dir == "" {
		fatalf("--dir is required")
	}
	// Optional exact key map (TSV: <flat_name>\t<key>).
	var keyMap map[string]string
	if o.keysFile != "" {
		f, err := os.Open(o.keysFile)
		if err != nil {
			fatalf("keys: %v", err)
		}
		defer f.Close()
		keyMap = map[string]string{}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) == 2 {
				keyMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}
	entries, err := os.ReadDir(o.dir)
	if err != nil {
		fatalf("--dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && (o.project == "" || strings.HasPrefix(e.Name(), o.project)) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		fmt.Println("// no files to migrate")
		return
	}
	uploaded, bytesTotal, ambiguous, failed := 0, int64(0), 0, 0
	for _, name := range names {
		src := filepath.Join(o.dir, name)
		key, _ := keyMap[name]
		exact := true
		if key == "" {
			var err error
			key, exact, err = keyFromFlatName(name)
			if err != nil {
				failed++
				fmt.Printf("SKIP  %s  %v\n", name, err)
				continue
			}
			if !exact {
				ambiguous++
			}
		}
		localMD5, err := md5File(src)
		if err != nil {
			failed++
			fmt.Printf("SKIP  %s  %v\n", name, err)
			continue
		}
		if o.dryRun {
			fmt.Printf("PUT   %s/%s  (local md5 %s)\n", o.bucket, key, localMD5)
			continue
		}
		// Read once, hash while uploading is not possible with a plain body;
		// the local md5 is available, send it as Content-MD5 and verify the
		// response ETag against it (SeaweedFS ETag == md5).
		f, err := os.Open(src)
		if err != nil {
			failed++
			fmt.Printf("SKIP  %s  %v\n", name, err)
			continue
		}
		st, _ := f.Stat()
		etag, perr := c.PutObject(o.bucket, key, f, "", localMD5)
		f.Close()
		if perr != nil {
			failed++
			fmt.Printf("FAIL  %s  %v\n", name, perr)
			continue
		}
		if etag != "" && etag != localMD5 {
			fmt.Printf("WARN  %s  etag %s != local md5 %s\n", name, etag, localMD5)
		}
		uploaded++
		if st != nil {
			bytesTotal += st.Size()
		}
		fmt.Printf("PUT   %s/%s\n", o.bucket, key)
	}
	fmt.Printf("// %d file(s) uploaded, %d bytes, %d ambiguous key(s), %d skipped/failed%s\n",
		uploaded, bytesTotal, ambiguous, failed, dryRunNote(o.dryRun))
	if failed > 0 {
		os.Exit(1)
	}
}

func fromSeaweed(c *s3x.Client, o opts) {
	if o.bucket == "" {
		fatalf("--bucket is required")
	}
	if o.dir == "" {
		fatalf("--dir is required")
	}
	if err := os.MkdirAll(o.dir, 0o755); err != nil {
		fatalf("--dir: %v", err)
	}
	objs, err := c.ListObjects(o.bucket, o.prefix)
	if err != nil {
		fatalf("list: %v", err)
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Key < objs[j].Key })
	done, failed := 0, 0
	for _, ob := range objs {
		gr, err := c.GetObject(o.bucket, ob.Key)
		if err != nil {
			failed++
			fmt.Printf("FAIL  %s  %v\n", ob.Key, err)
			continue
		}
		var out *os.File
		if !o.dryRun {
			p := filepath.Join(o.dir, flatFromKey(ob.Key))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				gr.Body.Close()
				failed++
				fmt.Printf("FAIL  %s  %v\n", ob.Key, err)
				continue
			}
			out, err = os.Create(p)
			if err != nil {
				gr.Body.Close()
				failed++
				fmt.Printf("FAIL  %s  %v\n", ob.Key, err)
				continue
			}
		}
		var dst io.Writer = io.Discard
		if out != nil {
			dst = out
		}
		h := md5.New()
		n, rerr := io.Copy(io.MultiWriter(dst, h), gr.Body)
		gr.Body.Close()
		if out != nil {
			out.Close()
		}
		if rerr != nil {
			failed++
			fmt.Printf("FAIL  %s  %v\n", ob.Key, rerr)
			continue
		}
		if o.verify {
			got := hex.EncodeToString(h.Sum(nil))
			if ob.ETag != "" && got != ob.ETag {
				fmt.Printf("WARN  %s  md5 %s != etag %s\n", ob.Key, got, ob.ETag)
			}
		}
		done++
		fmt.Printf("GET   %s/%s  (%d bytes)\n", o.bucket, ob.Key, n)
	}
	fmt.Printf("// %d object(s) fetched, %d failed%s\n", done, failed, dryRunNote(o.dryRun))
	if failed > 0 {
		os.Exit(1)
	}
}

func dryRunNote(dry bool) string {
	if dry {
		return "  [dry-run]"
	}
	return ""
}

// parseFlags reads the tool's --name value flags.
func parseFlags(args []string) opts {
	var o opts
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			i++
			if i >= len(args) {
				fatalf("missing value for %s", a)
			}
			return args[i]
		}
		switch a {
		case "--endpoint":
			o.endpoint = next()
		case "--bucket":
			o.bucket = next()
		case "--dir":
			o.dir = next()
		case "--prefix":
			o.prefix = next()
		case "--project":
			o.project = next()
		case "--keys":
			o.keysFile = next()
		case "--dry-run":
			o.dryRun = true
		case "--verify":
			o.verify = true
		default:
			fatalf("unknown flag %q", a)
		}
	}
	if o.endpoint == "" {
		o.endpoint = envOr("AWS_S3_ENDPOINT", envOr("OVERLEAF_FILESTORE_S3_ENDPOINT", "http://127.0.0.1:8333"))
	}
	return o
}
