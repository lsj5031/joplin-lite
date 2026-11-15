//go:build darwin || linux

package dropboxrepair

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func lockBackup(backup string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(backup, ".operator.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another repair operator holds this backup")
	}
	return func() { f.Close() }, nil // Closing releases even after a process crash.
}
