//go:build !unix

package editorai

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}
