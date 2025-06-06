package main

import (
	"syscall"

	"golang.org/x/xerrors"
)

// SetRLimitNoFile avoids too many open files error.
func SetRLimitNoFile() error {
	var rLimit syscall.Rlimit

	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		return xerrors.Errorf("could not get rlimit: %w", err)
	}

	if rLimit.Cur < rLimit.Max {
		rLimit.Cur = rLimit.Max
		err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
		if err != nil {
			return xerrors.Errorf("could not set rlimit: %w", err)
		}
	}

	return nil
}
