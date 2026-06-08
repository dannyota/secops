package cli

import "fmt"

// exitCoder carries a process exit code distinct from the default error code.
// Execute maps it to the process status so scripts and CI can branch on the
// outcome (git-style):
//
//	0  success / in sync
//	1  error (anything that went wrong)
//	2  divergence — state differs from desired (drift detected, or a dry-run
//	   that would change live state)
type exitCoder struct {
	code int
	err  error
}

func (e *exitCoder) Error() string { return e.err.Error() }
func (e *exitCoder) Unwrap() error { return e.err }
func (e *exitCoder) ExitCode() int { return e.code }

// divergence wraps a message as an exit-code-2 result: not an operational error,
// but a signal that live state differs from the local desired state (drift, or a
// dry-run that would change live). CI can treat 2 as "act", distinct from 1 "fix".
func divergence(format string, a ...any) error {
	return &exitCoder{code: 2, err: fmt.Errorf(format, a...)}
}
