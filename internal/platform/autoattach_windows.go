package platform

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// newAutoAttacher returns the Task Scheduler AutoAttacher (autoattach.go)
// wired to the real primitives.
func newAutoAttacher(t AutoAttachTarget) AutoAttacher {
	return &taskAttacher{target: t, ops: taskOps{
		run:         runPowerShell,
		executable:  os.Executable,
		getwd:       os.Getwd,
		currentUser: currentUser,
	}}
}

// currentUser returns DOMAIN\user of the current process token.
func currentUser() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("token user: %w", err)
	}
	account, domain, _, err := u.User.Sid.LookupAccount("")
	if err != nil {
		return "", fmt.Errorf("lookup account: %w", err)
	}
	return domain + `\` + account, nil
}
