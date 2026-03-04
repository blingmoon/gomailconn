package gomailconn

import "errors"

type ClientStatus string

const (
	ClientStatusInit     ClientStatus = "init"
	ClientStatusIniting  ClientStatus = "initing"
	ClientStatusRunning  ClientStatus = "running"
	ClientStatusStopped  ClientStatus = "stopped"
	ClientStatusAbnormal ClientStatus = "abnormal"
)

var (
	ErrNotRunning         = errors.New("client is not running")
	ErrAlreadyInitialized = errors.New("client is already initialized")
	ErrInvalidConfig      = errors.New("invalid config")
	ErrInvalidParams      = errors.New("invalid params")
)
