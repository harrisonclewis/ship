package localedit

import (
	"bytes"

	"github.com/harrisonclewis/ship/internal/config"
)

// ValidateShipYAML applies the same whole-file validation the TUI uses before
// saving ship.yaml.
func ValidateShipYAML(data []byte) error {
	_, err := config.Decode(bytes.NewReader(data))
	return err
}

// ValidateCompose applies the compose file checks ship already relies on.
func ValidateCompose(data []byte) error {
	return config.ValidateCompose(data)
}
