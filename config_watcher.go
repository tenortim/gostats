package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/fsnotify/fsnotify"
)

// startConfigWatcher watches configFileName for modifications and sends on the
// reload channel when a change is detected. Multiple rapid changes (e.g. an
// editor doing an atomic rename-over-write) are coalesced by a short debounce
// so that only one reload is triggered per save.
func startConfigWatcher(ctx context.Context, configFileName string, reload chan<- struct{}) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create config file watcher: %w", err)
	}
	if err := watcher.Add(configFileName); err != nil {
		watcher.Close()
		return fmt.Errorf("failed to watch config file %q: %w", configFileName, err)
	}
	log.Info("Watching config file for changes", slog.String("file", configFileName))
	go func() {
		defer watcher.Close()
		const debounceDelay = 500 * time.Millisecond
		var debounceTimer <-chan time.Time
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					debounceTimer = time.After(debounceDelay)
				}
				// Editors that write atomically (write temp file, rename over
				// target) produce a Rename or Remove event on the watched path.
				// Re-add the watch so we catch the new file.
				if event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
					_ = watcher.Add(configFileName)
					debounceTimer = time.After(debounceDelay)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Warn("config file watcher error", slog.String("error", err.Error()))
			case <-debounceTimer:
				debounceTimer = nil
				log.Log(ctx, LevelNotice, "config file changed - reloading",
					slog.String("file", configFileName))
				select {
				case reload <- struct{}{}:
				default: // reload already pending; skip
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}
