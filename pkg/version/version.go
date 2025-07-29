package version

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// Version is the current version of the cnpg-plugin-wal-g.
// This value is injected at build time by GoReleaser.
var version = "0.0.0"

// commitHash is short git commit hash for current revision
// This value is injected at build time by GoReleaser.
var commitHash = ""

// buildDate contains date when binary was built
// This value is injected at build time by GoReleaser.
var buildDate = ""

// Random suffix for development versions, will be empty for release builds
// This value is injected at build time by GoReleaser.
var devVersionSuffix = ""

func GetVersionNumber() string {
	return version
}

func GetCommitHash() string {
	return commitHash
}

func GetBuildDate() string {
	return buildDate
}

// GetVersion returns the current version of the cnpg-plugin-wal-g with commit hash.
func GetVersion() string {
	if strings.Contains(version, "SNAPSHOT") && devVersionSuffix == "" {
		// Add some random suffix for version on development to trigger updates
		devVersionSuffix = "-" + randomHex(8)
	}
	return version + devVersionSuffix
}

// randomHex generates a random hex string of the specified length.
func randomHex(length int) string {
	// Each byte gives two hex characters, so we need length/2 bytes
	bytes := make([]byte, (length+1)/2)
	_, err := rand.Read(bytes)
	if err != nil {
		return "abcdef"
	}

	hexStr := hex.EncodeToString(bytes)
	return hexStr[:length] // Trim to the exact length
}
