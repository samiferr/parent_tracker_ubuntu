package agent

import (
	"path/filepath"
	"testing"
)

func TestOnlyOneAgentPerUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.lock")
	release, got, err := Lock(path)
	if err != nil || !got {
		t.Fatalf("first lock: got=%v err=%v", got, err)
	}
	if _, got, err := Lock(path); err != nil || got {
		t.Fatalf("a second agent must not get the lock: got=%v err=%v", got, err)
	}
	release()
	release2, got, err := Lock(path)
	if err != nil || !got {
		t.Fatalf("the lock must be free once the first agent is gone: got=%v err=%v", got, err)
	}
	release2()
}

func TestLockFileFallsBackToRunUser(t *testing.T) {
	if got := LockFile("", 1000); got != "/run/user/1000/parent-tracker-agent.lock" {
		t.Fatalf("got %q", got)
	}
	if got := LockFile("/tmp/x", 1000); got != "/tmp/x/parent-tracker-agent.lock" {
		t.Fatalf("got %q", got)
	}
}
