package proxy

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func init() {
	if os.Getenv("CAGE_TEST_PROXY_CHILD") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
}

func TestStopDetachedProxySkipsMismatchedAndMissingStartTime(t *testing.T) {
	for _, startTime := range []string{"not-the-same-process", ""} {
		t.Run(startTime, func(t *testing.T) {
			root := t.TempDir()
			id := "stale-pid"
			writeRawState(t, root, id, State{PID: os.Getpid(), StartTime: startTime, Port: 1, HTTPPort: 2})

			if err := Stop(root, id); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(proxyStatePath(root, id)); !os.IsNotExist(err) {
				t.Fatalf("proxy.json should be removed, err=%v", err)
			}
		})
	}
}

func TestStopDetachedProxyKillsMatchingIdentity(t *testing.T) {
	root := t.TempDir()
	id := "matching-pid"
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "CAGE_TEST_PROXY_CHILD=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-wait:
		case <-time.After(time.Second):
		}
	})

	if err := WriteState(root, id, State{PID: cmd.Process.Pid, Port: 1, HTTPPort: 2}); err != nil {
		t.Fatal(err)
	}
	st, err := readState(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if st.StartTime == "" {
		t.Fatal("state should include process start time")
	}
	if err := Stop(root, id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wait:
	case <-time.After(5 * time.Second):
		t.Fatal("matching proxy process was not killed")
	}
}

func writeRawState(t *testing.T, root, id string, st State) {
	t.Helper()
	if err := os.MkdirAll(runDir(root, id), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir(root, id), "proxy.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}
