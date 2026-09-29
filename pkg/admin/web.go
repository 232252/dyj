package admin

import (
	"embed"
	"io/fs"
)

// The UI ships inside the binary: go:embed keeps dyj a single static
// file with no runtime assets directory. One control per file lives in
// web/components/, one page per file in web/pages/ (docs/research/10
// §2.3) — vanilla custom elements, no npm, no build chain.

//go:embed all:web
var embeddedWeb embed.FS

// DefaultWeb returns the embedded UI tree rooted at web/.
func DefaultWeb() fs.FS {
	sub, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		panic("admin: embedded web tree missing: " + err.Error())
	}
	return sub
}
