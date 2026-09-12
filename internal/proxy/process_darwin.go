//go:build darwin

package proxy

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

func processStartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid %d", pid)
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(err, unix.EIO) {
		return "", unix.ESRCH
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec), nil
}

func processNoLongerExists(err error) bool {
	return errors.Is(err, unix.ESRCH)
}

func stopRecorded(pid int, startTime string) error {
	if startTime == "" {
		return nil
	}
	ours, err := sameProcess(pid, startTime)
	if err != nil {
		return err
	}
	if !ours {
		return nil
	}
	kq, err := unix.Kqueue()
	if err != nil {
		return fmt.Errorf("create proxy process event queue: %w", err)
	}
	defer unix.Close(kq)
	changes := []unix.Kevent_t{{
		Ident:  uint64(pid),
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
	}}
	// register a watch for the process to exit
	if _, err := unix.Kevent(kq, changes, nil, nil); err != nil {
		return stopErr(pid, startTime, err, "watch proxy process %d: %w")
	}
	// check if it's still ours in case the PID is already gone or recycled
	ours, err = sameProcess(pid, startTime)
	if err != nil {
		return err
	}
	if !ours {
		return nil
	}
	// attempt to terminate the process
	if err := unix.Kill(pid, unix.SIGTERM); err != nil {
		return stopErr(pid, startTime, err, "terminate proxy process %d: %w")
	}
	events := make([]unix.Kevent_t, 1)
	// wait for the process to exit with timeout
	n, err := unix.Kevent(kq, nil, events, &unix.Timespec{Sec: 3})
	if err != nil {
		return stopErr(pid, startTime, err, "wait for proxy process %d after SIGTERM: %w")
	}
	if n > 0 {
		return nil
	}
	ours, err = sameProcess(pid, startTime)
	if err != nil {
		return err
	}
	if !ours {
		return nil
	}
	// if still here, repeat but forcefully with SIGKILL
	if err := unix.Kill(pid, unix.SIGKILL); err != nil {
		return stopErr(pid, startTime, err, "kill proxy process %d: %w")
	}
	n, err = unix.Kevent(kq, nil, events, &unix.Timespec{Sec: 3})
	if err != nil {
		return stopErr(pid, startTime, err, "wait for proxy process %d after SIGKILL: %w")
	}
	if n > 0 {
		return nil
	}
	ours, err = sameProcess(pid, startTime)
	if err != nil {
		return err
	}
	if !ours {
		return nil
	}
	return fmt.Errorf("proxy process %d is still running after SIGKILL", pid)
}
