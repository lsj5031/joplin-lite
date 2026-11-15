//go:build !darwin && !linux

package dropboxrepair

import "errors"

func lockBackup(string) (func(), error) {
	return nil, errors.New("remote apply requires macOS or Linux advisory locking")
}
