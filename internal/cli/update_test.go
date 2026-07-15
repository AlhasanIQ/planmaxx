package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/updater"
	"github.com/AlhasanIQ/planmaxx/internal/version"
)

func TestUpdateCommandInstallsLatestRelease(t *testing.T) {
	oldApplyUpdate := applyUpdate
	applyUpdate = func(context.Context, string) (updater.Status, bool, error) {
		return updater.Status{CurrentVersion: "0.2.0", LatestVersion: "0.3.0", UpdateAvailable: true}, true, nil
	}
	t.Cleanup(func() { applyUpdate = oldApplyUpdate })

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"update"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != "Updated PlanMaxx from 0.2.0 to 0.3.0.\n" {
		t.Fatalf("unexpected update output %q", got)
	}
}

func TestUpdateCommandReportsCurrentRelease(t *testing.T) {
	oldApplyUpdate := applyUpdate
	applyUpdate = func(context.Context, string) (updater.Status, bool, error) {
		return updater.Status{CurrentVersion: "0.3.0", LatestVersion: "0.3.0"}, false, nil
	}
	t.Cleanup(func() { applyUpdate = oldApplyUpdate })

	var stdout bytes.Buffer
	cmd := NewRootCommand(&stdout, &bytes.Buffer{})
	cmd.SetArgs([]string{"update"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != "PlanMaxx 0.3.0 is already up to date.\n" {
		t.Fatalf("unexpected update output %q", got)
	}
}

func TestUpdateCommandExplainsDevelopmentBuild(t *testing.T) {
	oldApplyUpdate := applyUpdate
	applyUpdate = func(context.Context, string) (updater.Status, bool, error) {
		return updater.Status{}, false, updater.ErrDevelopmentBuild
	}
	t.Cleanup(func() { applyUpdate = oldApplyUpdate })

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"update"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "development build") {
		t.Fatalf("expected development build explanation, got %v", err)
	}
}

func TestUpdateCommandWrapsFailure(t *testing.T) {
	oldApplyUpdate := applyUpdate
	applyUpdate = func(context.Context, string) (updater.Status, bool, error) {
		return updater.Status{}, false, errors.New("network unavailable")
	}
	t.Cleanup(func() { applyUpdate = oldApplyUpdate })

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"update"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "update PlanMaxx: network unavailable") {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestAgentUpdateNoticeAddressesCallingAgent(t *testing.T) {
	notice := agentUpdateNotice(updater.Status{CurrentVersion: "0.2.0", LatestVersion: "0.3.0"})
	for _, want := range []string{
		"Agent maintenance notice",
		"Tell the user",
		"planmaxx update",
	} {
		if !strings.Contains(notice, want) {
			t.Fatalf("expected notice to contain %q, got %q", want, notice)
		}
	}
}

func TestAutomaticUpdateCheckReturnsAgentNotice(t *testing.T) {
	t.Setenv("PLANMAXX_NO_UPDATE_CHECK", "")
	oldVersion := version.Version
	oldCheck := checkCachedUpdate
	version.Version = "0.2.0"
	checkCachedUpdate = func(context.Context, string, string, time.Duration, time.Time) (updater.Status, error) {
		return updater.Status{
			CurrentVersion:  "0.2.0",
			LatestVersion:   "0.3.0",
			UpdateAvailable: true,
		}, nil
	}
	t.Cleanup(func() {
		version.Version = oldVersion
		checkCachedUpdate = oldCheck
	})

	notice := <-beginAutomaticUpdateCheck(context.Background())
	if !strings.Contains(notice, "0.2.0 -> 0.3.0") || !strings.Contains(notice, "`planmaxx update`") {
		t.Fatalf("unexpected automatic update notice %q", notice)
	}
}

func TestAutomaticUpdateCheckCanBeDisabled(t *testing.T) {
	t.Setenv("PLANMAXX_NO_UPDATE_CHECK", "1")
	oldVersion := version.Version
	oldCheck := checkCachedUpdate
	version.Version = "0.2.0"
	checkCachedUpdate = func(context.Context, string, string, time.Duration, time.Time) (updater.Status, error) {
		t.Fatal("disabled update check should not call GitHub")
		return updater.Status{}, nil
	}
	t.Cleanup(func() {
		version.Version = oldVersion
		checkCachedUpdate = oldCheck
	})

	if notice := <-beginAutomaticUpdateCheck(context.Background()); notice != "" {
		t.Fatalf("expected no disabled update notice, got %q", notice)
	}
}
