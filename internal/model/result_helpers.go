// SPDX-License-Identifier: AGPL-3.0-only
package model

import (
	"encoding/json"

	"github.com/jolks/mcp-cron/internal/logging"
)

// PersistAndLogResult saves a result to the store (best-effort) and debug-logs it.
func PersistAndLogResult(store ResultStore, result *Result, logger *logging.Logger) {
	if store != nil {
		if err := store.SaveResult(result); err != nil {
			logger.Warnf("Failed to persist result for task %s: %v", result.TaskID, err)
		}
	}

	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		logger.Warnf("Failed to marshal result for task %s: %v", result.TaskID, err)
	} else {
		logger.Debugf("Task %s result: %s", result.TaskID, string(jsonData))
	}
}
