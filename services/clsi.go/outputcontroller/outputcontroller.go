// Package outputcontroller ports services/clsi/app/js/OutputController.js.
//
// Node parity:
//
//   - Route: GET /project/:project_id/build/:build_id/output/output.zip
//     GET /project/:project_id/user/:user_id/build/:build_id/output/output.zip
//
//   - Handler (expressify-wrapped):
//     const { project_id, user_id, build_id } = parseReq(req, schema, { logOnly: true })
//     const archive = await OutputFileArchiveManager.archiveFilesForBuild(projectId, userId, buildId)
//     res.attachment('output.zip')
//     res.setHeader('X-Content-Type-Options', 'nosniff')
//     await pipeline(archive, res)
//
//   - The strictObject schema:
//     project_id: objectId().or(submissionId())
//     user_id:    objectId().optional()
//     build_id:   buildId()
//     parsed with logOnly=true, so on an invalid/absent field the handler
//     proceeds with the (possibly empty) value and the archive manager
//     then throws NotFoundError for the missing content dir -> 404.
//
// Port: Go's http.ResponseWriter lets us stream only after we know the build
// succeeds, so we buffer the zip (bounded by the 7mb compile-size limit plus
// output files), map the archive manager's NotFoundError to 404 before
// writing any body, and on success emit the exact attachment headers Node's
// express sets.
package outputcontroller

import (
	"bytes"
	"errors"
	"net/http"
	"strconv"

	"clsi/outputfilearchivemanager"
)

// Request mirrors the parsed express params of one of the two output.zip
// routes. UserID is empty on the userless mount.
type Request struct {
	ProjectID string
	UserID    string
	BuildID   string
}

// CreateOutputZip streams the build's output.zip as an attachment and
// returns the HTTP status to record (always 200 on the happy path; 404 when
// the content dir / output files are missing — Node maps the archive
// manager's NotFoundError to a 404 JSON error before writing a body).
//
// Headers set on success (mirroring express res.attachment('output.zip')):
//
//	Content-Type:           application/octet-stream
//	Content-Disposition:    attachment; filename="output.zip"
//	X-Content-Type-Options: nosniff
func CreateOutputZip(res http.ResponseWriter, outputDir string, req Request) (int, error) {
	// Node: parseReq(..., { logOnly: true }) — proceed even if a field is
	// missing; the archive manager then surfaces the missing content dir.
	buf := &bytes.Buffer{}
	err := outputfilearchivemanager.ArchiveFilesForBuild(
		outputDir, req.ProjectID, req.UserID, req.BuildID, buf,
	)
	if err != nil {
		var nfe outputfilearchivemanager.NotFoundError
		if errors.As(err, &nfe) {
			return http.StatusNotFound, nil
		}
		// Non-NotFound error: log + 500 (expressify default for unhandled).
		return http.StatusInternalServerError, err
	}

	res.Header().Set("Content-Type", "application/octet-stream")
	res.Header().Set("Content-Disposition", `attachment; filename="output.zip"`)
	res.Header().Set("X-Content-Type-Options", "nosniff")
	res.Header().Set("Content-Length", strconv.Itoa(len(buf.Bytes())))
	res.WriteHeader(http.StatusOK)
	if _, werr := res.Write(buf.Bytes()); werr != nil {
		return http.StatusInternalServerError, werr
	}
	return http.StatusOK, nil
}
