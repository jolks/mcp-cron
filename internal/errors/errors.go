// SPDX-License-Identifier: AGPL-3.0-only
package errors

import (
	"fmt"
)

// NotFound creates a formatted "not found" error
func NotFound(resource, id string) error {
	return fmt.Errorf("resource not found: %s with ID %s", resource, id)
}

// AlreadyExists creates a formatted "already exists" error
func AlreadyExists(resource, id string) error {
	return fmt.Errorf("resource already exists: %s with ID %s", resource, id)
}

// InvalidInput creates a formatted "invalid input" error
func InvalidInput(reason string) error {
	return fmt.Errorf("invalid input: %s", reason)
}

// Internal creates a formatted "internal error" error
func Internal(err error) error {
	return fmt.Errorf("internal error: %v", err)
}
