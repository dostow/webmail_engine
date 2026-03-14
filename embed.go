package webmail_engine

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed frontend
var DistFS embed.FS

// GetDistFS returns the embedded dist directory as a filesystem
func GetDistFS() fs.FS {
	efs, err := fs.Sub(DistFS, "dist")
	if err != nil {
		panic(fmt.Sprintf("unable to serve frontend: %v", err))
	}
	return efs
}
