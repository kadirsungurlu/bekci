// Package web derlenmiş arayüz dosyalarını (web/dist) uygulamanın içine gömer.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist arayüz dosyaları; kökü dist/ klasörüdür.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
