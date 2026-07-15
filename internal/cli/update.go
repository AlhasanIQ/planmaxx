package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlhasanIQ/planmaxx/internal/updater"
	"github.com/AlhasanIQ/planmaxx/internal/version"

	"github.com/spf13/cobra"
)

const (
	automaticUpdateCheckInterval = 24 * time.Hour
	automaticUpdateCheckTimeout  = 3 * time.Second
	manualUpdateTimeout          = 2 * time.Minute
)

var (
	applyUpdate       = updater.Update
	checkCachedUpdate = updater.CheckCached
	beginUpdateCheck  = beginAutomaticUpdateCheck
)

func newUpdateCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update PlanMaxx to the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), manualUpdateTimeout)
			defer cancel()
			status, updated, err := applyUpdate(ctx, version.Version)
			if err != nil {
				if errors.Is(err, updater.ErrDevelopmentBuild) {
					return errors.New("cannot update a development build; install a released PlanMaxx binary first")
				}
				return fmt.Errorf("update PlanMaxx: %w", err)
			}
			if !updated {
				_, err = fmt.Fprintf(stdout, "PlanMaxx %s is already up to date.\n", status.CurrentVersion)
				return err
			}
			_, err = fmt.Fprintf(stdout, "Updated PlanMaxx from %s to %s.\n", status.CurrentVersion, status.LatestVersion)
			return err
		},
	}
}

func beginAutomaticUpdateCheck(parent context.Context) <-chan string {
	notice := make(chan string, 1)
	if version.Version == "dev" || updateChecksDisabled() {
		notice <- ""
		close(notice)
		return notice
	}

	go func() {
		defer close(notice)
		ctx, cancel := context.WithTimeout(parent, automaticUpdateCheckTimeout)
		defer cancel()

		cacheDir, err := userCacheDir()
		if err != nil || cacheDir == "" {
			notice <- ""
			return
		}
		status, err := checkCachedUpdate(
			ctx,
			version.Version,
			filepath.Join(cacheDir, "planmaxx", "update-check.json"),
			automaticUpdateCheckInterval,
			time.Now(),
		)
		if err != nil || !status.UpdateAvailable {
			notice <- ""
			return
		}
		notice <- agentUpdateNotice(status)
	}()
	return notice
}

func agentUpdateNotice(status updater.Status) string {
	return fmt.Sprintf(
		"Agent maintenance notice:\nA PlanMaxx update is available (%s -> %s). Tell the user briefly that an update exists for PlanMaxx. If they want it installed, run `planmaxx update`.",
		status.CurrentVersion,
		status.LatestVersion,
	)
}

func appendUpdateNotice(output string, notice <-chan string) string {
	message := <-notice
	if message == "" {
		return output
	}
	return strings.TrimRight(output, "\n") + "\n\n" + message + "\n"
}

func updateChecksDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PLANMAXX_NO_UPDATE_CHECK"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
