//go:build windows

package platform

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdll               = windows.NewLazySystemDLL("ntdll.dll")
	procNtResumeProcess = ntdll.NewProc("NtResumeProcess")
)

const processSuspendResume = 0x0800 // PROCESS_SUSPEND_RESUME

// winTree is a process tree confined to a Job Object with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE (SCOPE.md §7: CTRL_BREAK → deadline →
// TerminateJobObject).
type winTree struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error

	mu     sync.Mutex // guards job, proc and closed; serializes Cancel
	job    windows.Handle
	proc   windows.Handle
	closed bool
}

func startTree(cmd *exec.Cmd) (Tree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil { //nolint:gosec // G103: Win32 ABI requires a pointer to the struct
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure job: %w", err)
	}
	// Suspended: the child must be in the job before it runs any code, or a
	// fast child could spawn descendants that escape the job.
	// CREATE_NEW_PROCESS_GROUP lets Cancel target the tree with CTRL_BREAK.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_SUSPENDED,
	}
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("start tree: %w", err)
	}
	pid := cmd.Process.Pid
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|processSuspendResume,
		false, uint32(pid)) //nolint:gosec // G115: a Windows PID is a DWORD
	if err == nil {
		err = windows.AssignProcessToJobObject(job, h)
	}
	if err == nil {
		err = resumeProcess(h)
	}
	if err != nil {
		_ = cmd.Process.Kill() // still suspended; nothing else ran
		_ = cmd.Wait()         // release the os.Process handle
		if h != 0 {
			_ = windows.CloseHandle(h)
		}
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("start tree: %w", err)
	}
	t := &winTree{cmd: cmd, job: job, proc: h, done: make(chan struct{})}
	go func() { t.err = cmd.Wait(); close(t.done) }()
	return t, nil
}

// resumeProcess resumes every thread of a process created with
// CREATE_SUSPENDED. os/exec closes the primary thread handle, so the
// process-wide NtResumeProcess is used instead of ResumeThread.
func resumeProcess(h windows.Handle) error {
	if err := procNtResumeProcess.Find(); err != nil {
		return fmt.Errorf("NtResumeProcess: %w", err)
	}
	if st, _, _ := procNtResumeProcess.Call(uintptr(h)); st != 0 { // NTSTATUS
		return fmt.Errorf("NtResumeProcess: NTSTATUS 0x%x", st)
	}
	return nil
}

func (t *winTree) Pid() int { return t.cmd.Process.Pid }

// Wait waits for the root process, then closes the process and job handles.
// Closing the job also kills any straggler through KILL_ON_JOB_CLOSE.
func (t *winTree) Wait() error {
	<-t.done
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closeLocked()
	return t.err
}

// closeLocked closes the handles once. t.mu must be held.
func (t *winTree) closeLocked() {
	if t.closed {
		return
	}
	t.closed = true
	_ = windows.CloseHandle(t.proc)
	_ = windows.CloseHandle(t.job)
}

func (t *winTree) Cancel(grace time.Duration) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		// Wait already ran: the root exited and closing the job killed
		// every remaining member.
		return nil
	}
	// Best effort: fails without a shared console (CI, services) — the
	// deadline path covers it. PENDING U-PLAT-6: CTRL_BREAK when COWFlow runs
	// as a Windows service (no console).
	_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(t.cmd.Process.Pid)) //nolint:gosec // G115: a Windows PID is a DWORD
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-t.done:
	case <-timer.C:
	}
	// Kill every remaining member, including grandchildren that outlived the root.
	if err := windows.TerminateJobObject(t.job, 1); err != nil {
		return fmt.Errorf("terminate job: %w", err)
	}
	return nil
}
