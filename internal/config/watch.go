// Ver 2026-07-07 02:10, by Fable 5
package config

import (
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch invokes onChange (debounced) whenever the config file is written,
// created or renamed. It watches the parent directory because editors and
// tools typically replace the file rather than write it in place.
func Watch(path string, onChange func()) (func() error, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := watcher.Add(filepath.Dir(abs)); err != nil {
		watcher.Close()
		return nil, err
	}
	go func() {
		var timer *time.Timer
		for {
			select {
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Clean(ev.Name) != abs {
					continue
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(300*time.Millisecond, onChange)
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return watcher.Close, nil
}
