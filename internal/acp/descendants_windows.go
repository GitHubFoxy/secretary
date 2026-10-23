//go:build windows

package acp

type acpDescendantTracker struct{}

func startACPDescendantTracker(int) *acpDescendantTracker { return nil }
func (t *acpDescendantTracker) stop() {}
func prepareACPDescendantCleanup(*acpDescendantTracker) (func() error, error) { return nil, nil }
