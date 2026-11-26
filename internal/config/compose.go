package config

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ValidateCompose checks an app's hand-written compose.yaml before ship
// uploads it. It requires services and the ${IMAGE} placeholder ship fills in
// from /opt/apps/<app>/.ship.env.
func ValidateCompose(data []byte) error {
	var doc struct {
		Services map[string]struct {
			Image string `yaml:"image"`
		} `yaml:"services"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("compose file is empty")
		}
		return err
	}
	if len(doc.Services) == 0 {
		return errors.New("compose file has no services")
	}
	for _, svc := range doc.Services {
		if strings.Contains(svc.Image, "${IMAGE") {
			return nil
		}
	}
	return errors.New("no service uses image: ${IMAGE}")
}
