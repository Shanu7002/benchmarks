// Package sqltpl renders SQL from Go templates. Lookup order for a template
// name is sql/<dialect>/<name>.sql.tmpl, then sql/common/<name>.sql.tmpl, so a
// dialect only overrides what actually differs.
package sqltpl

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"github.com/Shanu7002/benchmarks/database/internal/driver"
)

//go:embed all:templates
var fs embed.FS

type Renderer struct {
	drv   driver.Driver
	funcs template.FuncMap
}

func New(drv driver.Driver) *Renderer {
	return &Renderer{drv: drv, funcs: template.FuncMap{
		"ph": drv.Placeholder,
		// values renders "(p1,p2),(p3,p4)..." for `rows` rows of `cols` columns.
		"values": func(rows, cols int) string {
			var sb strings.Builder
			n := 1
			for r := 0; r < rows; r++ {
				if r > 0 {
					sb.WriteByte(',')
				}
				sb.WriteByte('(')
				for c := 0; c < cols; c++ {
					if c > 0 {
						sb.WriteByte(',')
					}
					sb.WriteString(drv.Placeholder(n))
					n++
				}
				sb.WriteByte(')')
			}
			return sb.String()
		},
	}}
}

// Render executes the named template with data.
func (r *Renderer) Render(name string, data any) (string, error) {
	for _, dir := range []string{r.drv.Dialect(), "common"} {
		path := fmt.Sprintf("templates/%s/%s.sql.tmpl", dir, name)
		b, err := fs.ReadFile(path)
		if err != nil {
			continue
		}
		t, err := template.New(name).Funcs(r.funcs).Parse(string(b))
		if err != nil {
			return "", fmt.Errorf("parse %s: %w", path, err)
		}
		var out bytes.Buffer
		if err := t.Execute(&out, data); err != nil {
			return "", fmt.Errorf("render %s: %w", path, err)
		}
		return strings.TrimSpace(out.String()), nil
	}
	return "", fmt.Errorf("no template %q for dialect %q", name, r.drv.Dialect())
}
