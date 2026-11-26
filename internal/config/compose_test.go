package config

import (
	"strings"
	"testing"
)

func TestValidateCompose(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "app image placeholder",
			doc: `name: app
services:
  app:
    image: ${IMAGE:?run ship deploy app first}
`,
		},
		{
			name: "one app service among others",
			doc: `services:
  db:
    image: postgres:16
  app:
    image: ${IMAGE}
`,
		},
		{"empty", "", "empty"},
		{"invalid yaml", "services: [\n", "yaml"},
		{"no services", "name: app\n", "no services"},
		{"no image placeholder", "services:\n  app:\n    image: app:latest\n", "${IMAGE}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCompose([]byte(tc.doc))
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ValidateCompose: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
