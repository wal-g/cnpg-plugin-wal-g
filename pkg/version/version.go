package version

// Version is the current version of the cnpg-plugin-wal-g.
// This value is injected at build time by Go ldflags.
var version = ""

// commitHash is short git commit hash for current revision
// This value is injected at build time by Go ldflags.
var commitHash = ""

// buildDate contains date when binary was built
// This value is injected at build time by Go ldflags.
var buildDate = ""

// Returns true if current version is development
func IsDevelopment() bool {
	return version == "0.0.0-dev" || version == ""
}

func GetVersion() string {
	return version
}

func GetCommitHash() string {
	return commitHash
}

func GetBuildDate() string {
	return buildDate
}
