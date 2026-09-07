//go:build linux

package proxy

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

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

func stopMatchingProcess(pid int, startTime string) {
	if startTime == "" {
		return
	}
	current, err := processStartTime(pid)
	if err != nil || current != startTime {
		return
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return
	}
	defer unix.Close(fd)
	current, err = processStartTime(pid)
	if err != nil || current != startTime {
		return
	}
	_ = unix.PidfdSendSignal(fd, unix.SIGTERM, nil, 0)
	n, err := unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 3000)
	if err == nil && n == 0 {
		_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
	}
}
