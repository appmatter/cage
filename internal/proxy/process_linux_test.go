//go:build linux

package proxy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestStopClearsStateWhenProcessDiesBeforeSignal(t *testing.T) {
	root := t.TempDir()
	id := "gone-before-signal"
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "CAGE_TEST_PROXY_CHILD=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	if err := WriteState(root, id, State{PID: cmd.Process.Pid, Port: 1, HTTPPort: 2}); err != nil {
		t.Fatal(err)
	}

	oldSendSignal := pidfdSendSignal
	pidfdSendSignal = func(fd int, sig unix.Signal, info *unix.Siginfo, flags int) error {
		if sig == unix.SIGTERM {
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			<-wait
		}
		return oldSendSignal(fd, sig, info, flags)
	}
	defer func() { pidfdSendSignal = oldSendSignal }()

	if err := Stop(root, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(proxyStatePath(root, id)); !os.IsNotExist(err) {
		t.Fatalf("proxy.json should be removed, err=%v", err)
	}
}

func TestStartKeepsStateWhenStopFails(t *testing.T) {
	root := t.TempDir()
	id := "start-stop-fails"
	report := t.TempDir()
	t.Setenv("CAGE_TEST_PROXY_REPORT", report)
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
	if err := cmd.Process.Signal(unix.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	waitForStoppedProcess(t, cmd.Process.Pid)
	if err := WriteState(root, id, State{PID: cmd.Process.Pid, Port: 1, HTTPPort: 2}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readyPath(root, id), []byte("ready\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldSendSignal := pidfdSendSignal
	pidfdSendSignal = func(fd int, sig unix.Signal, info *unix.Siginfo, flags int) error {
		if sig == unix.SIGKILL {
			return unix.EPERM
		}
		return oldSendSignal(fd, sig, info, flags)
	}
	defer func() { pidfdSendSignal = oldSendSignal }()

	if _, err := Start(root, id, os.Args[0], StartOptions{}); err == nil {
		t.Fatal("Start should return the Stop error")
	}
	if _, err := os.Stat(proxyStatePath(root, id)); err != nil {
		t.Fatalf("proxy.json should remain after failed Start, err=%v", err)
	}
	if _, err := os.Stat(readyPath(root, id)); err != nil {
		t.Fatalf("proxy.ready should remain after failed Start, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(report, "argv")); !os.IsNotExist(err) {
		t.Fatalf("cageBin should not start, err=%v", err)
	}
}

func TestStopKeepsStateWhenKillCannotConfirmDeath(t *testing.T) {
	root := t.TempDir()
	id := "unkillable-pid"
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
	if err := cmd.Process.Signal(unix.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	waitForStoppedProcess(t, cmd.Process.Pid)
	if err := WriteState(root, id, State{PID: cmd.Process.Pid, Port: 1, HTTPPort: 2}); err != nil {
		t.Fatal(err)
	}

	oldSendSignal := pidfdSendSignal
	pidfdSendSignal = func(fd int, sig unix.Signal, info *unix.Siginfo, flags int) error {
		if sig == unix.SIGKILL {
			return unix.EPERM
		}
		return oldSendSignal(fd, sig, info, flags)
	}
	defer func() { pidfdSendSignal = oldSendSignal }()

	if err := Stop(root, id); err == nil {
		t.Fatal("Stop should fail when SIGKILL cannot be sent")
	}
	if _, err := os.Stat(proxyStatePath(root, id)); err != nil {
		t.Fatalf("proxy.json should remain after failed stop, err=%v", err)
	}
}

func waitForStoppedProcess(t *testing.T, pid int) {
	t.Helper()
	path := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stat, err := os.ReadFile(path)
		if err == nil && strings.HasPrefix(strings.TrimSpace(string(stat[strings.LastIndex(string(stat), ")")+1:])), "T") {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("process %d did not stop", pid)
}
