//go:build windows

package proxy

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func processStartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid %d", pid)
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	return processHandleStartTime(h)
}

func processHandleStartTime(h windows.Handle) (string, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return "", err
	}
	return fmt.Sprintf("%08x:%08x", creation.HighDateTime, creation.LowDateTime), nil
}

func stopMatchingProcess(pid int, startTime string) {
	if startTime == "" || pid <= 0 {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	current, err := processHandleStartTime(h)
	if err != nil || current != startTime {
		return
	}
	_ = windows.TerminateProcess(h, 1)
}
