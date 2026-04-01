package main

import "errors"

var (
	ErrIndexNotFound  = errors.New("remote index not found")
	ErrChunkLost      = errors.New("chunk lost on remote")
	ErrConfigInvalid  = errors.New("invalid configuration")
	ErrRestorePending = errors.New("restore still in progress")
	ErrMaxRetries     = errors.New("exceeded max retries")
	ErrUnknownAction  = errors.New("unknown action")
)
