// Package buildinfo exposes the source revision embedded at build time.
package buildinfo

// Revision is set by the image build. Unknown is deliberately not a verified SHA.
var Revision = "unknown"
