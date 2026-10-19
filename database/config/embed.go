// Package configfiles embeds the default config/*.yml so the dbbench binary stays
// self-contained (one static file copied to a VM), while still allowing an
// override directory (-settings-dir) when the values must change without a rebuild.
package configfiles

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

//go:embed defaults.yml limits.yml
var embedded embed.FS

// Decode reads name from dir when set and present, otherwise from the embedded
// copy, and decodes it strictly: unknown keys are errors, so a typo such as
// "max_row:" fails loudly instead of silently leaving a limit at zero.
// It returns where the values came from, for display.
func Decode(dir, name string, v any) (source string, err error) {
	var b []byte
	if dir != "" {
		path := filepath.Join(dir, name)
		b, err = os.ReadFile(path)
		switch {
		case err == nil:
			source = path
		case os.IsNotExist(err):
			b = nil
		default:
			return "", err
		}
	}
	if b == nil {
		if b, err = embedded.ReadFile(name); err != nil {
			return "", err
		}
		source = "embedded " + name
	}
	if err := Strict(b, v); err != nil {
		return "", fmt.Errorf("%s: %w", source, err)
	}
	return source, nil
}

// Strict decodes YAML, rejecting unknown fields.
func Strict(b []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	return dec.Decode(v)
}
