# `gitbridge/filestore`

the file-store adapters — ports `data/filestore/*` + its exceptions: read/write of project file blobs through the S3/SeaweedFS back-end (`s3x`).

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
