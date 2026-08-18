//go:build windows

package cli

import "testing"

func TestWindowsProcessListUsesNativeEnumeration(t *testing.T) {
	out, err := platformProcessList()
	if err != nil {
		t.Fatalf("enumerate Windows processes: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("Windows process enumeration returned no processes")
	}
}
