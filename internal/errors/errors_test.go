// SPDX-License-Identifier: AGPL-3.0-only
package errors

import (
	"fmt"
	"testing"
)

func TestNotFound(t *testing.T) {
	err := NotFound("task", "123")
	expectedMsg := "resource not found: task with ID 123"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestAlreadyExists(t *testing.T) {
	err := AlreadyExists("task", "123")
	expectedMsg := "resource already exists: task with ID 123"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestInvalidInput(t *testing.T) {
	reason := "missing required field"
	err := InvalidInput(reason)
	expectedMsg := "invalid input: " + reason
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestInternal(t *testing.T) {
	originalErr := fmt.Errorf("database connection failed")
	err := Internal(originalErr)
	expectedMsg := "internal error: database connection failed"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}
