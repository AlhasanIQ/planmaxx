//go:build windows

package cli

import "os/exec"

func platformProcessList() ([]byte, error) {
	const script = `$ErrorActionPreference = 'Stop'
Get-CimInstance Win32_Process | Where-Object { $_.CommandLine } | ForEach-Object {
  '{0} {1}' -f $_.ProcessId, $_.CommandLine
}`
	return exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script).Output()
}
