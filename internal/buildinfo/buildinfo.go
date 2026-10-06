package buildinfo

// These values are injected by release builds. Development builds deliberately
// report dev/unknown rather than pretending to be a packaged release.
var (
	Version   = "dev"
	Revision  = "unknown"
	BuildTime = "unknown"
)
