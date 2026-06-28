package cmd

import "os/exec"

// newCmd builds an exec.Cmd (centralized so commands can mock it in tests).
func newCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
