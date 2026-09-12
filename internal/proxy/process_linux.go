//go:build linux

package proxy

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

var pidfdSendSignal = unix.PidfdSendSignal

func processStartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid %d", pid)
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(stat[strings.LastIndex(string(stat), ")")+1:]))
	if len(fields) <= 19 {
		return "", fmt.Errorf("invalid /proc/%d/stat", pid)
	}
	return fields[19], nil
}

func processNoLongerExists(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH)
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
	// get the PID file descriptor to send signals and poll for exit
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return stopErr(pid, startTime, err, "open pidfd for proxy process %d: %w")
	}
	defer unix.Close(fd)
	// check if it's still ours in case the PID is already gone or recycled
	ours, err = sameProcess(pid, startTime)
	if err != nil {
		return err
	}
	if !ours {
		return nil
	}
	// attempt to terminate the process
	if err := pidfdSendSignal(fd, unix.SIGTERM, nil, 0); err != nil {
		return stopErr(pid, startTime, err, "terminate proxy process %d: %w")
	}
	// wait for the process to exit with timeout
	n, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 3000)
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
	if err := pidfdSendSignal(fd, unix.SIGKILL, nil, 0); err != nil {
		return stopErr(pid, startTime, err, "kill proxy process %d: %w")
	}
	n, err = unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 3000)
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
