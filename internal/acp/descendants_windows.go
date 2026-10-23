//go:build windows

package acp

func prepareACPDescendantCleanup(int) (func() error, error) {
	return func() error { return nil }, nil
}
