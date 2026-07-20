package updater

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

func InstallCandidate(candidatePath, binaryPath, previousPath string) (bool, error) {
	if filepath.Dir(binaryPath) != filepath.Dir(previousPath) {
		return false, errors.New("previous binary must be stored beside the service binary")
	}
	info, err := os.Stat(binaryPath)
	if err != nil || !info.Mode().IsRegular() {
		return false, errors.New("inspect installed binary failed")
	}
	metadata := fileMetadata{mode: info.Mode().Perm()}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		metadata.uid, metadata.gid = int(stat.Uid), int(stat.Gid)
		metadata.hasOwner = true
	}
	if err := copyFileAtomic(binaryPath, previousPath, metadata); err != nil {
		return false, errors.New("save previous binary failed")
	}
	if err := copyFileAtomic(candidatePath, binaryPath, metadata); err != nil {
		return true, errors.New("replace service binary failed")
	}
	return true, nil
}

func RestorePrevious(binaryPath, previousPath string) error {
	info, err := os.Stat(previousPath)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("previous binary is unavailable")
	}
	metadata := fileMetadata{mode: info.Mode().Perm()}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		metadata.uid, metadata.gid = int(stat.Uid), int(stat.Gid)
		metadata.hasOwner = true
	}
	return copyFileAtomic(previousPath, binaryPath, metadata)
}

type fileMetadata struct {
	mode     fs.FileMode
	uid      int
	gid      int
	hasOwner bool
}

func copyFileAtomic(source, destination string, metadata fileMetadata) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	dir := filepath.Dir(destination)
	temp, err := os.CreateTemp(dir, ".runeconsole-update-")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := io.Copy(temp, in); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if metadata.hasOwner {
		if err := temp.Chown(metadata.uid, metadata.gid); err != nil {
			return err
		}
	}
	if err := temp.Chmod(metadata.mode); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, destination); err != nil {
		return err
	}
	keep = true
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
