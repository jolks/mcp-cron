// SPDX-License-Identifier: AGPL-3.0-only
package singleton

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTryAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Acquire the lock.
	lock, isPrimary, err := TryAcquire(dbPath)
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if !isPrimary {
		t.Fatal("expected isPrimary=true")
	}
	if lock == nil {
		t.Fatal("expected non-nil lock")
	}

	// Release the lock.
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// Should be re-acquirable.
	lock2, isPrimary2, err := TryAcquire(dbPath)
	if err != nil {
		t.Fatalf("re-TryAcquire: %v", err)
	}
	if !isPrimary2 {
		t.Fatal("expected isPrimary=true on re-acquire")
	}
	defer func() { _ = lock2.Release() }()
}

// TestSecondInstanceIsSecondary verifies that when one process holds the lock,
// another process trying TryAcquire gets isPrimary=false.
func TestSecondInstanceIsSecondary(t *testing.T) {
	if os.Getenv("SINGLETON_HOLD_LOCK") == "1" {
		// Subprocess: acquire the lock and block until stdin is closed.
		dbPath := os.Getenv("SINGLETON_DB_PATH")
		lock, isPrimary, err := TryAcquire(dbPath)
		if err != nil || !isPrimary {
			os.Exit(2)
		}
		defer func() { _ = lock.Release() }()

		// Signal readiness by writing to a marker file.
		_ = os.WriteFile(dbPath+".ready", []byte("1"), 0o600)

		// Block until stdin is closed (parent will close it to signal exit).
		buf := make([]byte, 1)
		_, _ = os.Stdin.Read(buf)
		return
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Start a subprocess that holds the lock.
	cmd := exec.Command(os.Args[0], "-test.run=^TestSecondInstanceIsSecondary$")
	cmd.Env = append(os.Environ(),
		"SINGLETON_HOLD_LOCK=1",
		"SINGLETON_DB_PATH="+dbPath,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start subprocess: %v", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	// Wait for the subprocess to be ready.
	waitForFile(t, dbPath+".ready")

	// TryAcquire from this process — should get isPrimary=false.
	lock, isPrimary, err := TryAcquire(dbPath)
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if isPrimary {
		_ = lock.Release()
		t.Fatal("expected isPrimary=false when another process holds the lock")
	}
	if lock != nil {
		t.Fatal("expected nil lock for secondary")
	}
}

// TestTryAcquireCreatesDirectory verifies that TryAcquire creates parent
// directories if they don't exist (first-run scenario).
func TestTryAcquireCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent", "subdir")
	dbPath := filepath.Join(dir, "test.db")

	lock, isPrimary, err := TryAcquire(dbPath)
	if err != nil {
		t.Fatalf("TryAcquire with non-existent dir: %v", err)
	}
	if !isPrimary {
		t.Fatal("expected isPrimary=true")
	}
	defer func() { _ = lock.Release() }()
}

// TestStaleLock verifies that when a lock holder is killed without cleanup
// (simulating a crash), a new TryAcquire succeeds because the OS releases
// the flock.
func TestStaleLock(t *testing.T) {
	if os.Getenv("SINGLETON_HOLD_LOCK") == "1" {
		dbPath := os.Getenv("SINGLETON_DB_PATH")
		lock, _, err := TryAcquire(dbPath)
		if err != nil {
			os.Exit(2)
		}
		_ = lock // intentionally not releasing

		_ = os.WriteFile(dbPath+".ready", []byte("1"), 0o600)

		// Block forever — we expect to be SIGKILL'd.
		select {}
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Start a subprocess that holds the lock.
	cmd := exec.Command(os.Args[0], "-test.run=^TestStaleLock$")
	cmd.Env = append(os.Environ(),
		"SINGLETON_HOLD_LOCK=1",
		"SINGLETON_DB_PATH="+dbPath,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start subprocess: %v", err)
	}

	// Wait for ready.
	waitForFile(t, dbPath+".ready")

	// SIGKILL the subprocess — simulates a crash. The OS releases the flock.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill subprocess: %v", err)
	}
	_ = cmd.Wait()

	// TryAcquire should succeed (stale lock released by OS).
	lock, isPrimary, err := TryAcquire(dbPath)
	if err != nil {
		t.Fatalf("TryAcquire after crash: %v", err)
	}
	if !isPrimary {
		t.Fatal("expected isPrimary=true after crash")
	}
	defer func() { _ = lock.Release() }()
}

// waitForFile polls until path exists or 10 seconds elapse.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", path)
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}
