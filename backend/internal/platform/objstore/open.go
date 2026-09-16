package objstore

import (
	"fmt"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
)

// Opener builds a Store from config. It lives in a separate file so the
// interface file stays dependency-free; cmd/* call it.
type Opener func(cfg config.Config) (Store, error)

// registry maps config.ObjStore values to constructors. Implementations
// register themselves from init so this package never imports them.
var registry = map[string]Opener{}

// Register adds an implementation under a name.
func Register(name string, open Opener) { registry[name] = open }

// Open returns the store named by cfg.ObjStore.
func Open(cfg config.Config) (Store, error) {
	open, ok := registry[cfg.ObjStore]
	if !ok {
		return nil, fmt.Errorf("objstore: no implementation registered for %q (import it for its side effect)", cfg.ObjStore)
	}
	return open(cfg)
}
