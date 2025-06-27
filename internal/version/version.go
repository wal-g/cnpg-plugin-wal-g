package version

// Version is the current version of the cnpg-plugin-wal-g.
// This value is injected at build time by GoReleaser.
var Version = "dev"

// GetVersion returns the current version of the cnpg-plugin-wal-g.
func GetVersion() string {
	return Version
}
