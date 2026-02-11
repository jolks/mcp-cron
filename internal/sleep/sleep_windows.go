// SPDX-License-Identifier: AGPL-3.0-only
package sleep

import (
	"fmt"
	"runtime"
	"syscall"
)

const (
	esContinuous      = 0x80000000
	esSystemRequired  = 0x00000001
)

var procSetThreadExecutionState *syscall.LazyProc

func init() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
}

// Prevent prevents the system from idle sleeping using SetThreadExecutionState.
func Prevent() (func(), error) {
	done := make(chan struct{})
	errCh := make(chan error, 1)

	go func() {
		// Pin this goroutine to an OS thread because SetThreadExecutionState
		// is per-thread state.
		runtime.LockOSThread()

		ret, _, err := procSetThreadExecutionState.Call(uintptr(esContinuous | esSystemRequired))
		if ret == 0 {
			runtime.UnlockOSThread()
			errCh <- fmt.Errorf("SetThreadExecutionState: %w", err)
			return
		}
		errCh <- nil

		// Block until release is called. The locked OS thread keeps the
		// execution state assertion alive.
		<-done

		procSetThreadExecutionState.Call(uintptr(esContinuous))
		runtime.UnlockOSThread()
	}()

	if err := <-errCh; err != nil {
		return nil, err
	}

	release := func() {
		close(done)
	}
	return release, nil
}
