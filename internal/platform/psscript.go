package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"unicode/utf16"
	"unicode/utf8"
)

// PowerShell scripts for the Dev Drive steps that have no Win32 API
// (partition, format, mount) and for the Hyper-V fallback when virtdisk.dll
// is unavailable (SCOPE.md §6.4), and for the auto-attach scheduled task
// (§6.4 step 5). The scripts are fixed text. Values reach
// them only as an int, a uint64, or base64 of an already validated string,
// so no user-provided text ever appears unencoded in PowerShell source
// (SCOPE.md §7, §17). Rendering is OS-independent so it is tested
// everywhere; running the scripts lives in psscript_windows.go.
//
// Every script starts with psPrelude: errors terminate the script (and give
// powershell.exe a non-zero exit code), and progress records are silenced so
// they do not reach stderr as CLIXML.

const psPrelude = `$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
`

// formatScript initializes disk {{.Disk}} as GPT, creates one partition
// over the whole disk, formats it as a Dev Drive (ReFS) and mounts it at
// the decoded mount (a drive letter or a directory access path).
const formatScript = psPrelude + `$n = [int]'{{.Disk}}'
$mount = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.MountB64}}'))
Initialize-Disk -Number $n -PartitionStyle GPT
if ($mount -match '^[D-Z]:$') {
  $p = New-Partition -DiskNumber $n -UseMaximumSize -DriveLetter $mount[0]
  Format-Volume -DriveLetter $mount[0] -DevDrive -Confirm:$false | Out-Null
} else {
  $p = New-Partition -DiskNumber $n -UseMaximumSize
  Format-Volume -Partition $p -DevDrive -Confirm:$false | Out-Null
  Add-PartitionAccessPath -DiskNumber $n -PartitionNumber $p.PartitionNumber -AccessPath $mount
}
`

// mountScript mounts the basic data partition of disk {{.Disk}} at the
// decoded mount. The partition is chosen by its GPT type GUID (basic data),
// which is not localized, so the reserved partition is never picked.
const mountScript = psPrelude + `$n = [int]'{{.Disk}}'
$mount = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.MountB64}}'))
$p = Get-Partition -DiskNumber $n | Where-Object { $_.GptType -eq '{ebd0a0a2-b9e5-4433-87c0-68b6b72699c7}' } | Select-Object -First 1
if ($null -eq $p) { throw "disk $n has no data partition" }
if ($mount -match '^[D-Z]:$') {
  Set-Partition -DiskNumber $n -PartitionNumber $p.PartitionNumber -NewDriveLetter $mount[0]
} else {
  Add-PartitionAccessPath -DiskNumber $n -PartitionNumber $p.PartitionNumber -AccessPath $mount
}
`

// The Hyper-V fallback scripts (New-VHD needs the Hyper-V PowerShell module,
// which Windows Home lacks).
const (
	newVHDScript = psPrelude + `$path = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.PathB64}}'))
New-VHD -Path $path -SizeBytes ([uint64]'{{.Size}}') -Dynamic | Out-Null
`
	// mountVHDScript prints the disk number of the attached VHDX.
	mountVHDScript = psPrelude + `$path = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.PathB64}}'))
(Mount-VHD -Path $path -Passthru | Get-Disk).Number
`
	dismountVHDScript = psPrelude + `$path = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.PathB64}}'))
Dismount-VHD -Path $path
`
	// vhdDiskScript prints the disk number of the VHDX, or -1 when it is
	// not attached.
	vhdDiskScript = psPrelude + `$path = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.PathB64}}'))
$v = Get-VHD -Path $path
if ($v.Attached) { $v.DiskNumber } else { -1 }
`
)

// The Dev Drive auto-attach scripts (SCOPE.md §6.4 step 5): one Task
// Scheduler task per VHDX under the fixed folder \COWFlow\. Register uses
// -Force, so registering again replaces the task; unregister removes the
// task only when it exists. Both are therefore idempotent.
const (
	// registerTaskScript registers a task triggered at logon of the
	// decoded user, running as that user with the highest privileges:
	// Execute = the decoded cowflow.exe path, Argument = the fixed text
	// "devdrive attach", WorkingDirectory = the decoded project root. The
	// battery settings let the task start on a laptop running on battery
	// (Task Scheduler's defaults would skip it).
	registerTaskScript = psPrelude + `$name = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.NameB64}}'))
$exe = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.ExeB64}}'))
$dir = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.DirB64}}'))
$user = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.UserB64}}'))
$action = New-ScheduledTaskAction -Execute $exe -Argument 'devdrive attach' -WorkingDirectory $dir
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $user
$principal = New-ScheduledTaskPrincipal -UserId $user -LogonType Interactive -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskPath '\COWFlow\' -TaskName $name -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Force | Out-Null
`
	// unregisterTaskScript removes the decoded task when it exists.
	unregisterTaskScript = psPrelude + `$name = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{{.NameB64}}'))
if ($null -ne (Get-ScheduledTask -TaskPath '\COWFlow\' -TaskName $name -ErrorAction SilentlyContinue)) {
  Unregister-ScheduledTask -TaskPath '\COWFlow\' -TaskName $name -Confirm:$false
}
`
)

// scriptArgs are the only values a script template can see. Strings are
// carried exclusively as base64.
type scriptArgs struct {
	Disk     int
	Size     uint64
	MountB64 string
	PathB64  string
	NameB64  string
	ExeB64   string
	DirB64   string
	UserB64  string
}

// b64 encodes s as base64 of its UTF-8 bytes; the alphabet contains no
// quote, so the result is safe inside a single-quoted PowerShell literal.
func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// renderScript fills the fixed script src with args.
func renderScript(src string, args scriptArgs) (string, error) {
	t, err := template.New("ps").Option("missingkey=error").Parse(src)
	if err != nil {
		return "", fmt.Errorf("powershell script template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, args); err != nil {
		return "", fmt.Errorf("powershell script template: %w", err)
	}
	return buf.String(), nil
}

// renderFormatScript renders formatScript for disk n and an already
// validated mount.
func renderFormatScript(n int, mount string) (string, error) {
	return renderScript(formatScript, scriptArgs{Disk: n, MountB64: b64(mount)})
}

// renderMountScript renders mountScript for disk n and an already validated
// mount.
func renderMountScript(n int, mount string) (string, error) {
	return renderScript(mountScript, scriptArgs{Disk: n, MountB64: b64(mount)})
}

// renderPathScript renders one of the Hyper-V fallback scripts for an
// already validated VHDX path and size.
func renderPathScript(src, path string, size uint64) (string, error) {
	return renderScript(src, scriptArgs{PathB64: b64(path), Size: size})
}

// renderRegisterTaskScript renders registerTaskScript for task name and an
// already validated target.
func renderRegisterTaskScript(name string, t AutoAttachTarget) (string, error) {
	return renderScript(registerTaskScript, scriptArgs{
		NameB64: b64(name), ExeB64: b64(t.Exe), DirB64: b64(t.WorkDir), UserB64: b64(t.User),
	})
}

// renderUnregisterTaskScript renders unregisterTaskScript for task name.
func renderUnregisterTaskScript(name string) (string, error) {
	return renderScript(unregisterTaskScript, scriptArgs{NameB64: b64(name)})
}

// encodeCommand returns script in the form powershell.exe -EncodedCommand
// expects: base64 of its UTF-16LE encoding.
func encodeCommand(script string) string {
	enc := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(enc))
	for i, u := range enc {
		binary.LittleEndian.PutUint16(b[2*i:], u)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// parseScriptInt parses the single integer a script printed on stdout.
func parseScriptInt(out string) (int, error) {
	s := strings.TrimSpace(out)
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("powershell: expected one integer on stdout, got %q", truncate(s, 256))
	}
	return n, nil
}

// truncate shortens s to at most n bytes without splitting a UTF-8
// sequence, marking the cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "...(truncated)"
}
