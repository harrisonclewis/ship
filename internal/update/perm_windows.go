//go:build windows

package update

func isReadOnlyError(error) bool {
	return false
}
