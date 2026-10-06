package nodepower

import "context"

type unsupportedInhibitor struct{ name string }

func (u unsupportedInhibitor) Supported() bool { return false }
func (u unsupportedInhibitor) Name() string    { return u.name }
func (u unsupportedInhibitor) Acquire(context.Context, string) (InhibitHandle, error) {
	return nil, ErrInhibitorUnsupported
}
