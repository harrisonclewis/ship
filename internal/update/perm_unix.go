//go:build !windows

package update

import (
	"errors"
	"syscall"
)

func isReadOnlyError(err error) bool {
	return errors.Is(err, syscall.EROFS)
}
