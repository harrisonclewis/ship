package update

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.2.0", -1},
		{"0.2.0", "0.1.0", 1},
		{"0.2.0", "0.2.0", 0},
		{"v0.2.0", "0.2.0", 0},
		{"1.10.0", "1.9.0", 1},
		{"1.2.3", "1.2.10", -1},
	}
	for _, test := range tests {
		got, err := compareVersions(test.a, test.b)
		if err != nil {
			t.Fatalf("compareVersions(%q, %q): %v", test.a, test.b, err)
		}
		if (got < 0) != (test.want < 0) || (got > 0) != (test.want > 0) || (got == 0) != (test.want == 0) {
			t.Errorf("compareVersions(%q, %q) = %d, want sign %d", test.a, test.b, got, test.want)
		}
	}

	for _, pair := range [][2]string{{"dev", "1.0.0"}, {"1.0", "1.0.0"}, {"1.0.0-dirty", "1.0.0"}} {
		if _, err := compareVersions(pair[0], pair[1]); err == nil {
			t.Errorf("compareVersions(%q, %q) succeeded, want an error", pair[0], pair[1])
		}
	}
}

func TestAssetName(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "ship-linux-x64"},
		{"linux", "arm64", "ship-linux-arm64"},
		{"darwin", "arm64", "ship-darwin-arm64"},
		{"darwin", "amd64", "ship-darwin-x64"},
		{"windows", "amd64", "ship-windows-x64.exe"},
		{"windows", "arm64", "ship-windows-arm64.exe"},
		{"freebsd", "amd64", ""},
		{"linux", "386", ""},
		{"windows", "386", ""},
	}
	for _, test := range tests {
		if got := AssetName(test.goos, test.goarch); got != test.want {
			t.Errorf("AssetName(%q, %q) = %q, want %q", test.goos, test.goarch, got, test.want)
		}
	}
}
