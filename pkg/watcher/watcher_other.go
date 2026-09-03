//go:build !windows

package watcher

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// startRootWatch is the non-Windows fallback. fsnotify has no recursive mode and
// no overflow signal, so this is an honest stub: the root and the directories
// that exist when Monitoramento starts are registered, new directories are
// registered as they are created, and Overflow is never reported.
//
// ScanFile Pro targets Windows; watcher_windows.go is the real implementation.
func startRootWatch(root string, bufSize int, sink changeSink) (func(), error) {
	_ = bufSize // fsnotify owns its own buffer

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := addTreeRecursive(w, root); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("não foi possível observar %q: %w", root, err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case event, ok := <-w.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Create) {
					if info, statErr := os.Lstat(event.Name); statErr == nil && info.IsDir() {
						_ = w.Add(event.Name)
					}
				}
				if sink.Change != nil {
					sink.Change(event.Name, event.Has(fsnotify.Rename))
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return func() {
		_ = w.Close()
		<-done
	}, nil
}

// addTreeRecursive registers root and every directory below it, bounded by what
// exists at start time.
func addTreeRecursive(w *fsnotify.Watcher, root string) error {
	if err := w.Add(root); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable branches are skipped, never fatal
		}
		if d.IsDir() && path != root {
			_ = w.Add(path)
		}
		return nil
	})
}
