//go:build windows

package proxy

import (
	"fmt"
	"os/exec"
)

func configureDetachedProcess(cmd *exec.Cmd) error {
	return fmt.Errorf("proxy detach is not implemented on windows")
}
