// Ver 2026-07-07 02:10, by Fable 5
package config

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch invokes onChange (debounced) whenever the config file is written,
// created or renamed. It watches the parent directory because editors and
// tools typically replace the file rather than write it in place. onError,
// if non-nil, is called for every error fsnotify reports on its Errors
// channel (an exhausted inotify handle, the watched directory disappearing,
// …) — without it, the watch goroutine used to just read the error and drop
// it, so hot reload could stop working with zero signal to the operator;
// SIGHUP would still work, but nothing said so. It is also called if the
// watch goroutine itself panics (recovered there) — the goroutine cannot be
// revived, so that report means hot reload is down until restart. onError
// runs on the same internal goroutine as onChange, so it must not block.
func Watch(path string, onChange func(), onError func(error)) (func() error, error) {
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
		defer func() {
			// A watcher goroutine that dies silently leaves hot reload broken
			// with zero signal (SIGHUP still works). Nothing here is expected
			// to panic, but if it does, surface it through the same channel
			// fsnotify errors use — and note the goroutine is gone for good:
			// hot reload stays down until restart.
			if p := recover(); p != nil && onError != nil {
				onError(fmt.Errorf("hot-reload watcher panicked; hot reload is down until restart: %v", p))
			}
		}()
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
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				if onError != nil {
					onError(err)
				}
			}
		}
	}()
	return watcher.Close, nil
}
