# `gitbridge/swap`

the S3 swap store — ports `S3SwapStore` (the AWS-S3-backed blob store used to swap large blobs in/out of the working tree; region defaulting + `s3x` client).

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
