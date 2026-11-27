package config

import "fmt"

// NormalizeArch converts `uname -m` output into a docker platform value.
func NormalizeArch(machine string) (string, error) {
	switch machine {
	case "x86_64", "amd64", "linux/amd64":
		return "linux/amd64", nil
	case "aarch64", "arm64", "linux/arm64":
		return "linux/arm64", nil
	default:
		return "", fmt.Errorf("unsupported CPU architecture %q (want x86_64 or aarch64)", machine)
	}
}
