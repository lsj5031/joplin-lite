//go:build !s3

package sync

import (
	"errors"
	"fmt"

	"github.com/lsj5031/joplin-lite/internal/config"
)

// ErrS3Disabled means this binary was built without the S3 driver.
var ErrS3Disabled = errors.New("S3 sync target is not compiled into this build")

// NewS3Backend exists so `serve`/`sync` can report a useful error instead of
// failing to compile. S3 (aws-sdk-go-v2) is behind the `s3` build tag; the
// default build stays small for the Dropbox-first targets.
func NewS3Backend(*config.Config) (SyncBackend, error) {
	return nil, fmt.Errorf("%w: rebuild with `go build -tags s3`", ErrS3Disabled)
}
