package ws

import (
	"github.com/klarkxy/nekonest/relaycore"
	"github.com/nekonest/server/internal/buildinfo"
	"github.com/nekonest/server/internal/db"
)

// NewCore composes the standalone shell with the public single-nest engine.
func NewCore(database *db.DB, phoneSecret string) *relaycore.Engine {
	engine, err := relaycore.NewEngine(relaycore.Config{
		Store:           database,
		PhoneSecret:     phoneSecret,
		AppVersion:      buildinfo.Version,
		DuplicateDaemon: relaycore.ReplaceExisting,
	})
	if err != nil {
		panic(err)
	}
	return engine
}
