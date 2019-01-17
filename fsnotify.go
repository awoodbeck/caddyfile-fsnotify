// +build !windows

package fsnotify

import (
	"errors"
	"flag"
	"log"
	"os"
	"sync"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/mholt/caddy"
	"github.com/mholt/caddy/caddyfile"
)

var (
	notify  bool
	done    chan struct{}
	watcher *fsnotify.Watcher
	wg      sync.WaitGroup
)

func init() {
	flag.BoolVar(&notify, "notify", false, "Notify Caddy of config file changes to prompt a restart")
	caddy.RegisterCaddyfileLoader("fsnotify", caddy.LoaderFunc(load))
}

func load(serverType string) (caddy.Input, error) {
	if notify {
		caddy.RegisterEventHook(caddy.CaddyfileParsedEvent, handler)
	}

	return nil, nil
}

func handler(event caddy.EventName, data interface{}) error {
	if event != caddy.CaddyfileParsedEvent {
		return nil
	}

	sbs, ok := data.(caddyfile.ServerBlocks)
	if !ok {
		return errors.New("unexpected event handler data")
	}

	if watcher != nil {
		select {
		case done <- struct{}{}:
		default:
		}
		wg.Wait()
		watcher.Close()
	}

	var err error
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	for _, f := range sbs.Files() {
		err = watcher.Add(f)
		if err != nil {
			log.Printf("[ERROR] unable to watch file %q: %s", f, err)
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case event := <-watcher.Events:
				log.Printf("[INFO] fsnotify event: %v", event)
				pid := os.Getpid()
				p, err := os.FindProcess(pid)
				if err != nil {
					log.Printf("[ERROR] unable to find process ID %d", pid)
					continue
				}
				err = p.Signal(syscall.SIGUSR1)
				if err != nil {
					log.Printf("[ERROR] sending reload signal: %s", err)
					continue
				}
			case err := <-watcher.Errors:
				log.Printf("[ERROR] fsnotify: %s", err)
			case <-done:
				return
			}
		}
	}()

	return nil
}
