package templates

import (
	"embed"
	htmltemplate "html/template"
)

//go:embed *.html
var htmlFiles embed.FS

// MustParse returns the embedded HTML template set.
func MustParse() *htmltemplate.Template {
	return htmltemplate.Must(htmltemplate.New("").ParseFS(htmlFiles, "*.html"))
}
