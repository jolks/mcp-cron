// SPDX-License-Identifier: AGPL-3.0-only
package sleep

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

// Prevent prevents the system from idle sleeping by spawning caffeinate.
// The caffeinate process watches our PID and exits automatically when we do.
func Prevent() (func(), error) {
	cmd := exec.Command("caffeinate", "-i", "-w", strconv.Itoa(os.Getpid()))
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start caffeinate: %w", err)
	}

	release := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}
	return release, nil
}
