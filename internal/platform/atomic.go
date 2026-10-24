package platform

import (
	"fmt"
	"log"
	"os"
	"time"
)

// WriteFileAtomic writes via a temporary file and rename so a process kill
// cannot leave a truncated JSON file behind.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// BackupCorruptFile renames an unparsable data file to a timestamped backup so
// a later save cannot silently overwrite the original content.
func BackupCorruptFile(path string, cause error) {
	backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
	if err := os.Rename(path, backup); err != nil {
		log.Printf("CORRUPT data file %s (%v); rename to backup failed: %v", path, cause, err)
		return
	}
	log.Printf("CORRUPT data file %s (%v); backed up to %s", path, cause, backup)
}
