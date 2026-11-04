package server

import "os"

// Release builds stamp these values with -ldflags; runtime overrides still win.
var (
	buildVersion = "unknown"
	buildCommit  = "development"
)

func buildInfo() (string, string) {
	version, ok := os.LookupEnv("APP_VERSION")
	if !ok {
		version = buildVersion
	}
	commit, ok := os.LookupEnv("GIT_SHA")
	if !ok {
		commit = buildCommit
	}
	return version, commit
}
