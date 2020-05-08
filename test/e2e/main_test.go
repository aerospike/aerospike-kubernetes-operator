package e2e

import (
	"testing"

	log "github.com/inconshreveable/log15"
	f "github.com/operator-framework/operator-sdk/pkg/test"
)

const logLevel = log.LvlInfo

// LevelFilterHandler filters log messages based on the current log level.
func LevelFilterHandler(h log.Handler) log.Handler {
	return log.FilterHandler(func(r *log.Record) (pass bool) {
		return r.Lvl <= logLevel
	}, h)
}

// setupLogger sets up the logger from the config.
func setupLogger() error {
	handler := log.Root().GetHandler()
	handler = log.CallerFileHandler(handler)
	handler = LevelFilterHandler(handler)
	log.Root().SetHandler(handler)
	return nil
}
func TestMain(m *testing.M) {
	setupLogger()
	f.MainEntry(m)
}
