//go:build !s3

// Package s3 is the AWS S3 target driver. It is behind the `s3` build tag so
// the default binary (Dropbox / Joplin Server / local folder) does not link
// the aws-sdk-go-v2 tree. Build with `-tags s3` to enable sync target 8.
package s3
