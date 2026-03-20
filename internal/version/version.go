package version

// Version and BuildTime are injected at build time via -ldflags.
// Defaults are used when running without the build pipeline (e.g. go run).
var (
	Version   = "dev"
	BuildTime = "unknown"
)
