package ws

import "errors"

var (
	ErrDeviceOffline = errors.New("device is offline")
	ErrInvalidToken  = errors.New("invalid device token")
)
