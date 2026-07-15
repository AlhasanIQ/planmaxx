package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AlhasanIQ/planmaxx/internal/planfile"
	"github.com/AlhasanIQ/planmaxx/internal/review"
	"github.com/AlhasanIQ/planmaxx/internal/revisions"
	"github.com/spf13/cobra"
)

var processList = func() ([]byte, error) {
	return exec.Command("ps", "-axo", "pid=,command=").Output()
}

type planStoragePaths struct {
	plan          string
	bundle        string
	legacyJSON    []string
	legacyBundles []string
	legacyStores  []string
}

func resolvePlanStoragePaths(planArg string) (planStoragePaths, error) {
	plan, err := planfile.Load(planArg)
	if err != nil {
		return planStoragePaths{}, err
	}
	document := review.NewDocument(plan.Path, plan.Markdown)
	stateRoot, err := userStateDir()
	if err != nil {
		return planStoragePaths{}, err
	}
	cachePath, err := cacheAutosavePath(document.CanonicalPath)
	if err != nil {
		return planStoragePaths{}, err
	}
	paths := planStoragePaths{
		plan: document.CanonicalPath, bundle: review.BundlePath(stateRoot, document.CanonicalPath),
		legacyJSON:    []string{defaultAutosavePath(document.CanonicalPath), cachePath},
		legacyBundles: []string{filepath.Join(filepath.Dir(document.CanonicalPath), ".planmaxx", "revisions.bundle")},
	}
	if dataDir, err := userDataDir(); err == nil && dataDir != "" {
		paths.legacyStores = append(paths.legacyStores, filepath.Join(dataDir, "planmaxx", "revisions.git"))
	}
	if cacheDir, err := userCacheDir(); err == nil && cacheDir != "" {
		paths.legacyStores = append(paths.legacyStores, filepath.Join(cacheDir, "planmaxx", "revisions.git"))
	}
	return paths, nil
}

func newDoctorCommand(stdout io.Writer) *cobra.Command {
	var bundleOverride string
	cmd := &cobra.Command{
		Use:   "doctor <plan-file>",
		Short: "Inspect review storage, migration, and lock state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := resolvePlanStoragePaths(args[0])
			if err != nil {
				return err
			}
			if bundleOverride != "" {
				paths.bundle = bundleOverride
			}
			fmt.Fprintf(stdout, "Plan: %s\nBundle: %s\n", paths.plan, paths.bundle)
			info, err := review.InspectStorageFile(paths.bundle)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Bundle kind: %s\n", info.Kind)
			bundleCurrent := info.Kind == review.StorageFileBundle
			if bundleCurrent {
				store, err := review.OpenBundleStore(paths.bundle)
				if err != nil {
					fmt.Fprintf(stdout, "Bundle validity: invalid (%v)\n", err)
				} else {
					if saved, ok := store.Load(); ok {
						fmt.Fprintf(stdout, "Bundle validity: valid\nGeneration: %d\nStatus: %s\n", saved.Generation, saved.Status)
					} else {
						fmt.Fprintln(stdout, "Bundle validity: invalid (state ref has no review state)")
					}
					_ = store.Close()
				}
			}
			held, marker, err := review.ProbeBundleLock(paths.bundle)
			if err != nil {
				fmt.Fprintf(stdout, "Bundle write lock: unknown (%v)\n", err)
			} else if held {
				fmt.Fprintf(stdout, "Bundle write lock: held (%s)\n", marker)
			} else {
				fmt.Fprintf(stdout, "Bundle write lock: free (%s)\n", marker)
			}

			processes, processErr := matchingReviewProcesses(paths.plan)
			if processErr != nil {
				fmt.Fprintf(stdout, "Matching review processes: unavailable (%v)\n", processErr)
			} else if len(processes) == 0 {
				fmt.Fprintln(stdout, "Matching review processes: none detected")
			} else {
				fmt.Fprintln(stdout, "Matching review processes:")
				for _, process := range processes {
					fmt.Fprintf(stdout, "  %s\n", process)
				}
			}

			legacy := inspectLegacyStorage(paths)
			if len(legacy.items) == 0 {
				fmt.Fprintln(stdout, "Legacy storage: none")
			} else {
				fmt.Fprintln(stdout, "Legacy storage:")
				for _, item := range legacy.items {
					fmt.Fprintf(stdout, "  %s\n", item)
				}
			}
			lockCount := legacyLockMarkerCount(paths)
			fmt.Fprintf(stdout, "Legacy lock markers: %d\n", lockCount)
			switch {
			case !bundleCurrent && legacy.migrationAvailable:
				fmt.Fprintf(stdout, "Recommendation: run planmaxx review %q once to import legacy storage. Originals will be preserved.\n", paths.plan)
			case bundleCurrent && len(legacy.items) > 0:
				fmt.Fprintln(stdout, "Cleanup candidates: the legacy files above, after confirming no older PlanMaxx process still uses them. PlanMaxx will not delete them automatically.")
			case len(legacy.items) > 0:
				fmt.Fprintln(stdout, "Recommendation: legacy artifacts were found, but none is a matching automatic migration source. Review them manually; PlanMaxx will not delete them.")
			default:
				fmt.Fprintln(stdout, "Recommendation: no storage migration is needed.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&bundleOverride, "bundle", "", "inspect this bundle instead of the default plan bundle")
	return cmd
}

func newSnapshotCommand(stdout io.Writer) *cobra.Command {
	var out string
	var force bool
	var bundleOverride string
	cmd := &cobra.Command{
		Use:     "snapshot <plan-file>",
		Aliases: []string{"export"},
		Short:   "Copy the verified review bundle to a portable snapshot",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := resolvePlanStoragePaths(args[0])
			if err != nil {
				return err
			}
			if bundleOverride != "" {
				paths.bundle = bundleOverride
			}
			if out == "" {
				return errors.New("--out is required")
			}
			if err := review.SnapshotBundle(paths.bundle, out, force); err != nil {
				return fmt.Errorf("create review snapshot: %w", err)
			}
			fmt.Fprintf(stdout, "PlanMaxx snapshot: %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "write the verified .planmaxx snapshot here")
	cmd.Flags().StringVar(&bundleOverride, "bundle", "", "snapshot this bundle instead of the default plan bundle")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing snapshot")
	return cmd
}

func matchingReviewProcesses(planPath string) ([]string, error) {
	out, err := processList()
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "planmaxx review") && (strings.Contains(trimmed, planPath) || strings.Contains(trimmed, filepath.Base(planPath))) {
			matches = append(matches, trimmed)
		}
	}
	return matches, nil
}

type legacyStorageReport struct {
	items              []string
	migrationAvailable bool
}

func inspectLegacyStorage(paths planStoragePaths) legacyStorageReport {
	planRef := revisions.PlanRef(revisions.PlanID(paths.plan))
	var report legacyStorageReport
	seen := map[string]bool{}
	for _, path := range append(append([]string{}, paths.legacyJSON...), paths.legacyBundles...) {
		info, err := review.InspectStorageFile(path)
		if err == nil && info.Kind != review.StorageFileMissing {
			detail := string(info.Kind)
			matching := info.Kind == review.StorageFileLegacyJSON || (info.Kind == review.StorageFileLegacyBundle && info.HasRef(planRef))
			if info.Kind == review.StorageFileLegacyBundle && !matching {
				detail += ", no matching plan ref"
			}
			report.items = append(report.items, fmt.Sprintf("%s (%s)", path, detail))
			report.migrationAvailable = report.migrationAvailable || matching
			seen[path] = true
		}
	}
	for _, path := range adjacentLegacyArtifacts(paths.plan) {
		if seen[path] {
			continue
		}
		info, err := review.InspectStorageFile(path)
		if err == nil && info.Kind == review.StorageFileLegacyJSON {
			report.items = append(report.items, fmt.Sprintf("%s (manual JSON snapshot)", path))
			seen[path] = true
		}
	}
	handoff := filepath.Join(filepath.Dir(paths.plan), ".planmaxx", "handoff.md")
	if _, err := os.Stat(handoff); err == nil {
		report.items = append(report.items, handoff+" (legacy generated handoff)")
	}
	for _, path := range paths.legacyStores {
		store, ok, err := revisions.OpenExisting(path)
		if err == nil && ok {
			matching := store.HasPlan(revisions.PlanID(paths.plan))
			detail := "legacy shared repository"
			if !matching {
				detail += ", no matching plan ref"
			}
			report.items = append(report.items, fmt.Sprintf("%s (%s)", path, detail))
			report.migrationAvailable = report.migrationAvailable || matching
		}
	}
	return report
}

func adjacentLegacyArtifacts(planPath string) []string {
	matches, _ := filepath.Glob(planPath + ".planmaxx-review*")
	var artifacts []string
	for _, path := range matches {
		if strings.HasSuffix(path, ".lock") {
			continue
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			artifacts = append(artifacts, path)
		}
	}
	return artifacts
}

func legacyLockMarkerCount(paths planStoragePaths) int {
	count := 0
	for _, path := range paths.legacyJSON {
		if _, err := os.Stat(path + ".lock"); err == nil {
			count++
		}
	}
	for _, root := range paths.legacyStores {
		parent := filepath.Dir(root)
		_ = filepath.WalkDir(parent, func(path string, entry fs.DirEntry, err error) error {
			if err == nil && !entry.IsDir() && strings.HasSuffix(entry.Name(), ".lock") {
				count++
			}
			return nil
		})
	}
	return count
}
