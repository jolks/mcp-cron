// SPDX-License-Identifier: AGPL-3.0-only
//go:build !darwin && !windows

package sleep

import "fmt"

// Prevent is a no-op on unsupported platforms.
func Prevent() (func(), error) {
	return nil, fmt.Errorf("sleep prevention is not supported on this platform")
}
