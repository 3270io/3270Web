// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// stateNames is everything that belongs to a deployment rather than to the
// program: accounts, credentials, the trail, and everyone's saved work.
//
// Listed rather than "move everything", because the same directory holds the
// binary and web/ on a desktop install, and a migration that swept those into
// the data folder would break the next start.
var stateNames = []string{
	"users.json",
	"api-tokens.json",
	"audit.log",
	"audit.log.1",
	".env",
	"3270Web.log",
	"chaos-runs",
	"chaos-hints.json",
	"tasks.json",
	"connection-profiles.json",
	"themes",
	"snapshots",
	"screen-traces",
	printerSpoolDirName,
	"skills",
	"instructions",
	"extensions",
	userDataDirName,
	sharedDataDirName,
}

// migrateStateToDataDir moves an older installation's files into the data
// directory the first time one is configured.
//
// Setting DATA_DIR on an instance that already has accounts would otherwise
// look exactly like losing them: the server reads an empty directory, finds no
// accounts, and comes back up in first-run setup. The files are still there,
// beside the program, which is no comfort to somebody staring at a setup page
// where their sign-in used to be.
//
// Only ever from the program's own directory into an empty data directory. If
// the data directory already has state it is authoritative and nothing is
// touched — a second instance pointed at the same folder must not have another
// instance's files shovelled into it.
func migrateStateToDataDir(installDir, dataDir string) error {
	if installDir == "" || dataDir == "" || filepath.Clean(installDir) == filepath.Clean(dataDir) {
		return nil
	}
	if hasState(dataDir) {
		return nil
	}
	present := statePresentIn(installDir)
	if len(present) == 0 {
		return nil
	}

	log.Printf("data: found an older installation's files beside the program; "+
		"moving %d item(s) into %s", len(present), dataDir)

	moved := make([]string, 0, len(present))
	for _, name := range present {
		from := filepath.Join(installDir, name)
		to := filepath.Join(dataDir, name)
		if err := movePath(from, to); err != nil {
			return fmt.Errorf("move %s into the data directory: %w", name, err)
		}
		moved = append(moved, name)
	}

	// Said plainly and at the top level, because somebody upgrading needs to
	// know their accounts moved rather than discover it.
	log.Printf("data: moved %s into %s. This happens once, when a data "+
		"directory is first configured; the files are the same ones, in a "+
		"place a container rebuild cannot delete", strings.Join(moved, ", "), dataDir)
	fmt.Fprintf(os.Stderr,
		"3270Web: moved this installation's accounts and saved work into %s (%s).\n"+
			"They were beside the program, where a container rebuild deletes them.\n",
		dataDir, strings.Join(moved, ", "))
	return nil
}

// hasState reports whether a directory already holds a deployment's files.
func hasState(dir string) bool {
	return len(statePresentIn(dir)) > 0
}

// statePresentIn lists which of the known state files exist in a directory.
func statePresentIn(dir string) []string {
	found := make([]string, 0, len(stateNames))
	for _, name := range stateNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			found = append(found, name)
		}
	}
	return found
}

// movePath renames, and copies when a rename cannot cross the boundary.
//
// The interesting case is the one that matters here: the old location is very
// often a mounted volume and the new one is not, so the two are different
// filesystems and os.Rename fails with EXDEV. A migration that gave up there
// would leave exactly the installations this exists to rescue.
func movePath(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrExist) && !isCrossDevice(err) {
		return err
	}
	if err := copyPath(from, to); err != nil {
		return err
	}
	return os.RemoveAll(from)
}

func isCrossDevice(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		// EXDEV has no portable constant reachable without syscall, and the
		// message is stable enough for a fallback that is only ever slower.
		return strings.Contains(strings.ToLower(linkErr.Err.Error()), "cross-device")
	}
	return false
}

// copyPath copies a file or a directory tree, preserving permissions.
func copyPath(from, to string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	switch {
	case info.IsDir():
		if err := os.MkdirAll(to, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(from)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(from, entry.Name()), filepath.Join(to, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	case info.Mode()&os.ModeSymlink != 0:
		// Left behind rather than followed: a link into the image layer would
		// not mean the same thing once the image is replaced.
		return nil
	default:
		src, err := os.Open(from)
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		defer dst.Close()
		if _, err := io.Copy(dst, src); err != nil {
			return err
		}
		return dst.Sync()
	}
}
