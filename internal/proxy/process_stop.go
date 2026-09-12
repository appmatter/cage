package proxy

import (
	"fmt"
)

func sameProcess(pid int, startTime string) (ours bool, err error) {
	current, err := processStartTime(pid)
	if err != nil {
		if processNoLongerExists(err) {
			return false, nil
		}
		return false, fmt.Errorf("read proxy process %d start time: %w", pid, err)
	}
	return current == startTime, nil
}

// Something failed: if the process is already gone or recycled, treat that as success; if it's still ours, keep the error.
func stopErr(pid int, startTime string, op error, msg string) error {
	if processNoLongerExists(op) {
		return nil
	}
	ours, err := sameProcess(pid, startTime)
	if err != nil {
		return err
	}
	if !ours {
		return nil
	}
	return fmt.Errorf(msg, pid, op)
}

func stopMatchingProcess(pid int, startTime string) error {
	return stopRecorded(pid, startTime)
}
