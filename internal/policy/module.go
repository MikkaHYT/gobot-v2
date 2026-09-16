package policy

import (
	"strings"
	"sync"

	"gobot/internal/database"
)

type Options struct {
	ConfiguredGlobalUsers []string
}

type Module struct {
	db              dbDriver
	configuredUsers map[string]struct{}
	mutationMu      sync.Mutex
}

func New(db *database.DB, opts Options) *Module {
	return newWithDriver(newProdDB(db), opts)
}

func newWithDriver(driver dbDriver, opts Options) *Module {
	userSet := make(map[string]struct{}, len(opts.ConfiguredGlobalUsers))
	for _, u := range opts.ConfiguredGlobalUsers {
		trimmed := strings.TrimSpace(u)
		if trimmed != "" {
			userSet[trimmed] = struct{}{}
		}
	}
	return &Module{
		db:              driver,
		configuredUsers: userSet,
	}
}
