package clock

import "time"

type Clock interface {
	Now() time.Time
	UnixMilli() int64
}

type Real struct{}

func (Real) Now() time.Time   { return time.Now().UTC() }
func (Real) UnixMilli() int64 { return time.Now().UTC().UnixMilli() }
