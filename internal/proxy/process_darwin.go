//go:build darwin

package proxy

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func processStartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid %d", pid)
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec), nil
}

func stopMatchingProcess(pid int, startTime string) {
	if startTime == "" {
		return
	}
	current, err := processStartTime(pid)
	if err != nil || current != startTime {
		return
	}
	kq, err := unix.Kqueue()
	if err != nil {
		return
	}
	defer unix.Close(kq)
	changes := []unix.Kevent_t{{
		Ident:  uint64(pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}}
	if _, err := unix.Kevent(kq, changes, nil, nil); err != nil {
		return
	}
	current, err = processStartTime(pid)
	if err != nil || current != startTime {
		return
	}
	_ = unix.Kill(pid, unix.SIGTERM)
	events := make([]unix.Kevent_t, 1)
	n, err := unix.Kevent(kq, nil, events, &unix.Timespec{Sec: 3})
	if err != nil || n > 0 {
		return
	}
	current, err = processStartTime(pid)
	if err == nil && current == startTime {
		_ = unix.Kill(pid, unix.SIGKILL)
	}
}
