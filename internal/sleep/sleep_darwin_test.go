// SPDX-License-Identifier: AGPL-3.0-only
package sleep

import (
	"os/exec"
	"testing"
	"time"
)

func TestPrevent(t *testing.T) {
	// Skip if caffeinate is not available (shouldn't happen on macOS)
	if _, err := exec.LookPath("caffeinate"); err != nil {
		t.Skip("caffeinate not found")
	}

	release, err := Prevent()
	if err != nil {
		t.Fatalf("Prevent() returned error: %v", err)
	}

	// Give caffeinate a moment to start
	time.Sleep(100 * time.Millisecond)

	// Release should not panic
	release()

	// Calling release again should be safe (process already killed)
	release()
}
