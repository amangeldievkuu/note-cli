// Package atomicfile writes files so readers never see partial contents.
package atomicfile

import (
	"os"
	"path/filepath"
)

// TempPrefix starts the name of every temp file this package creates, so
// directory scanners and watchers can skip them.
const TempPrefix = ".tmp-"

// Write writes data to a temp file in the same directory as path, syncs it,
// and renames it over path.
func Write(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), TempPrefix+"*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, perm)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
