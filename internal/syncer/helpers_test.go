package syncer

import (
	"os"
	"path/filepath"
)

func makeGitDirectory(path string) error {
	return os.MkdirAll(filepath.Join(path, ".git"), 0o755)
}
