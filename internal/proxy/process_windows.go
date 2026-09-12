//go:build windows

package proxy

import (
	"errors"
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

func processNoLongerExists(err error) bool {
	return errors.Is(err, windows.ERROR_INVALID_PARAMETER)
}

func stopRecorded(pid int, startTime string) error {
	if startTime == "" || pid <= 0 {
		return nil
	}
	ours, err := sameProcess(pid, startTime)
	if err != nil {
		return err
	}
	if !ours {
		return nil
	}
	// open the process handle to send signals and poll for exit
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if processNoLongerExists(err) {
			return nil
		}
		return fmt.Errorf("open proxy process %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	// check if it's still ours in case the PID is already gone or recycled
	ours, err = sameProcess(pid, startTime)
	if err != nil {
		return err
	}
	if !ours {
		return nil
	}
	// attempt to terminate the process
	if err := windows.TerminateProcess(h, 1); err != nil {
		return stopErr(pid, startTime, err, "terminate proxy process %d: %w")
	}
	// wait for the process to exit with timeout
	status, err := windows.WaitForSingleObject(h, 3000)
	if err != nil {
		return stopErr(pid, startTime, err, "wait for proxy process %d: %w")
	}
	if status == windows.WAIT_OBJECT_0 {
		return nil
	}
	ours, err = sameProcess(pid, startTime)
	if err != nil {
		return err
	}
	if !ours {
		return nil
	}
	if status == uint32(windows.WAIT_TIMEOUT) {
		return fmt.Errorf("proxy process %d is still running after termination", pid)
	}
	return fmt.Errorf("wait for proxy process %d: unexpected status %d", pid, status)
}
