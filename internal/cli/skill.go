package cli

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	skillTargetCodex = "codex"
	skillFileName    = "SKILL.md"

	planmaxxReminderStart = "<!-- planmaxx skill reminder:start -->"
	planmaxxReminderEnd   = "<!-- planmaxx skill reminder:end -->"
	planmaxxReminderBody  = "## PlanMaxx Skill\nWhen the PlanMaxx skill is installed, use it whenever an agent-written plan is ready for user review. Check the `planmaxx` skill before proceeding from planning to implementation."

	planmaxxManagedSkillMarker = "<!-- planmaxx-managed-skill -->"
)

//go:embed SKILL.md
var defaultSkillTemplate []byte

var (
	skillTemplateEmbedded = append([]byte(nil), defaultSkillTemplate...)
	skillUserHomeDir      = os.UserHomeDir
	skillUserConfigDir    = os.UserConfigDir
)

// SetEmbeddedSkillTemplate replaces the embedded skill template.
// Tests use this to keep install/remove behavior deterministic.
func SetEmbeddedSkillTemplate(b []byte) {
	skillTemplateEmbedded = append([]byte(nil), b...)
}

func newSkillCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill <install|remove>",
		Short: "Install or remove the optional PlanMaxx Codex skill",
	}
	cmd.AddCommand(newSkillInstallCommand(stderr))
	cmd.AddCommand(newSkillRemoveCommand(stderr))
	return cmd
}

func newSkillInstallCommand(stderr io.Writer) *cobra.Command {
	opts := skillInstallOptions{target: skillTargetCodex}
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install PlanMaxx as an optional Codex skill",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillInstall(opts, stderr)
		},
	}
	cmd.Flags().StringVar(&opts.target, "target", opts.target, "skill target; currently codex")
	cmd.Flags().StringVar(&opts.repo, "repo", "", "install inside this repository instead of the user-level Codex directory")
	cmd.Flags().StringVar(&opts.source, "source", "", "local SKILL.md source path")
	cmd.Flags().BoolVar(&opts.copyMode, "copy", false, "copy SKILL.md instead of symlinking")
	cmd.Flags().BoolVar(&opts.linkMode, "link", false, "symlink SKILL.md instead of copying")
	return cmd
}

func newSkillRemoveCommand(stderr io.Writer) *cobra.Command {
	opts := skillRemoveOptions{target: skillTargetCodex}
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a PlanMaxx Codex skill installed by PlanMaxx",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillRemove(opts, stderr)
		},
	}
	cmd.Flags().StringVar(&opts.target, "target", opts.target, "skill target; currently codex")
	cmd.Flags().StringVar(&opts.repo, "repo", "", "remove from this repository instead of the user-level Codex directory")
	cmd.Flags().BoolVar(&opts.keepReminder, "keep-reminder", false, "leave the PlanMaxx reminder block in AGENTS.md")
	return cmd
}

type skillInstallOptions struct {
	target   string
	repo     string
	source   string
	copyMode bool
	linkMode bool
}

type skillRemoveOptions struct {
	target       string
	repo         string
	keepReminder bool
}

func runSkillInstall(opts skillInstallOptions, stderr io.Writer) error {
	if err := validateSkillTarget(opts.target); err != nil {
		return err
	}
	sourceBytes, sourcePath, sourceLabel, err := loadSkillSource(opts.source)
	if err != nil {
		return err
	}
	repoRoot, err := resolveSkillRepoRoot(opts.repo)
	if err != nil {
		return err
	}
	destination, reminderFile, err := resolveCodexSkillPaths(repoRoot)
	if err != nil {
		return err
	}

	linkMode := opts.linkMode
	if !opts.linkMode && !opts.copyMode {
		linkMode = runtime.GOOS != "windows"
	}
	if opts.copyMode {
		linkMode = false
	}

	mode := "copy"
	if linkMode {
		mode = "symlink"
	}
	scope := "user"
	if repoRoot != "" {
		scope = "repo: " + repoRoot
	}
	fmt.Fprintf(stderr, "Installing PlanMaxx skill (%s mode, %s) from %s\n", mode, scope, sourceLabel)
	installedPath, err := installSkillFile(destination, sourceBytes, sourcePath, linkMode)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Installed %s\n", installedPath)

	changed, err := ensurePlanmaxxReminder(reminderFile)
	if err != nil {
		return fmt.Errorf("update reminder in %s: %w", reminderFile, err)
	}
	if changed {
		fmt.Fprintf(stderr, "Updated PlanMaxx reminder in %s\n", reminderFile)
	} else {
		fmt.Fprintf(stderr, "PlanMaxx reminder already present in %s\n", reminderFile)
	}
	return nil
}

func runSkillRemove(opts skillRemoveOptions, stderr io.Writer) error {
	if err := validateSkillTarget(opts.target); err != nil {
		return err
	}
	repoRoot, err := resolveSkillRepoRoot(opts.repo)
	if err != nil {
		return err
	}
	destination, reminderFile, err := resolveCodexSkillPaths(repoRoot)
	if err != nil {
		return err
	}
	managedSource, err := defaultManagedSkillSourcePath()
	if err != nil {
		return err
	}

	targetFile := filepath.Join(destination, skillFileName)
	removed, skipped, err := removeManagedSkillFile(targetFile, managedSource)
	if err != nil {
		return err
	}
	switch {
	case removed:
		fmt.Fprintf(stderr, "Removed %s\n", targetFile)
	case skipped:
		fmt.Fprintf(stderr, "Skipped unmanaged skill at %s\n", targetFile)
	default:
		fmt.Fprintf(stderr, "PlanMaxx skill not found at %s\n", targetFile)
	}
	_ = removeEmptyDir(destination)

	if !opts.keepReminder {
		changed, err := removePlanmaxxReminder(reminderFile)
		if err != nil {
			return fmt.Errorf("remove reminder from %s: %w", reminderFile, err)
		}
		if changed {
			fmt.Fprintf(stderr, "Removed PlanMaxx reminder from %s\n", reminderFile)
		}
	}
	return nil
}

func validateSkillTarget(raw string) error {
	target := strings.ToLower(strings.TrimSpace(raw))
	if target == "" || target == skillTargetCodex {
		return nil
	}
	return fmt.Errorf("target must be codex")
}

func loadSkillSource(sourceRaw string) ([]byte, string, string, error) {
	if strings.TrimSpace(sourceRaw) != "" {
		path, err := expandHomePath(sourceRaw)
		if err != nil {
			return nil, "", "", err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, "", "", err
		}
		if len(bytes.TrimSpace(b)) == 0 {
			return nil, "", "", fmt.Errorf("empty skill source: %s", path)
		}
		return b, path, path, nil
	}

	managedPath, err := defaultManagedSkillSourcePath()
	if err != nil {
		return nil, "", "", err
	}
	if len(bytes.TrimSpace(skillTemplateEmbedded)) == 0 {
		return nil, "", "", fmt.Errorf("embedded skill template is empty")
	}
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
		return nil, "", "", err
	}

	existing, readErr := os.ReadFile(managedPath)
	if readErr == nil && bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(skillTemplateEmbedded)) {
		return existing, managedPath, managedPath, nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, "", "", readErr
	}

	status := "seeded from embedded template"
	if readErr == nil {
		status = "updated from embedded template"
	}
	if err := os.WriteFile(managedPath, skillTemplateEmbedded, 0o644); err != nil {
		return nil, "", "", err
	}
	return skillTemplateEmbedded, managedPath, fmt.Sprintf("%s (%s)", managedPath, status), nil
}

func defaultManagedSkillSourcePath() (string, error) {
	configDir, err := skillUserConfigDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(configDir) == "" {
		return "", fmt.Errorf("user config directory is empty")
	}
	return filepath.Join(configDir, "planmaxx", skillFileName), nil
}

func resolveSkillRepoRoot(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	root, err := expandHomePath(raw)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("--repo must be a directory")
	}
	return root, nil
}

func resolveCodexSkillPaths(repoRoot string) (string, string, error) {
	if repoRoot != "" {
		return filepath.Join(repoRoot, ".agents", "skills", "planmaxx"),
			filepath.Join(repoRoot, "AGENTS.md"),
			nil
	}

	home, err := skillUserHomeDir()
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(home) == "" {
		return "", "", fmt.Errorf("home directory is empty")
	}
	return filepath.Join(home, ".agents", "skills", "planmaxx"),
		filepath.Join(home, ".codex", "AGENTS.md"),
		nil
}

func installSkillFile(destinationDir string, sourceBytes []byte, sourcePath string, linkMode bool) (string, error) {
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return "", err
	}
	targetFile := filepath.Join(destinationDir, skillFileName)
	if linkMode {
		absSourcePath, err := filepath.Abs(sourcePath)
		if err != nil {
			return "", err
		}
		if err := os.Remove(targetFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err := os.Symlink(absSourcePath, targetFile); err != nil {
			return "", err
		}
		return targetFile, nil
	}

	tmpFile := targetFile + ".tmp"
	if err := os.WriteFile(tmpFile, sourceBytes, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmpFile, targetFile); err != nil {
		_ = os.Remove(tmpFile)
		return "", err
	}
	return targetFile, nil
}

func removeManagedSkillFile(targetFile string, managedSource string) (removed bool, skipped bool, err error) {
	info, err := os.Lstat(targetFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, false, nil
		}
		return false, false, err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err := os.Readlink(targetFile)
		if err != nil {
			return false, false, err
		}
		if isManagedSkillLink(linkTarget, managedSource) {
			return true, false, os.Remove(targetFile)
		}
		return false, true, nil
	}

	b, err := os.ReadFile(targetFile)
	if err != nil {
		return false, false, err
	}
	if isManagedSkillContent(b) {
		return true, false, os.Remove(targetFile)
	}
	return false, true, nil
}

func isManagedSkillLink(linkTarget string, managedSource string) bool {
	absTarget, targetErr := filepath.Abs(linkTarget)
	absManaged, managedErr := filepath.Abs(managedSource)
	if targetErr != nil || managedErr != nil {
		return false
	}
	return filepath.Clean(absTarget) == filepath.Clean(absManaged)
}

func isManagedSkillContent(content []byte) bool {
	if bytes.Contains(content, []byte(planmaxxManagedSkillMarker)) {
		return true
	}
	return bytes.Equal(bytes.TrimSpace(content), bytes.TrimSpace(skillTemplateEmbedded))
}

func ensurePlanmaxxReminder(path string) (bool, error) {
	return upsertManagedBlock(path, strings.Join([]string{
		planmaxxReminderStart,
		planmaxxReminderBody,
		planmaxxReminderEnd,
	}, "\n"))
}

func removePlanmaxxReminder(path string) (bool, error) {
	currentBytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	current := string(currentBytes)
	start := strings.Index(current, planmaxxReminderStart)
	end := strings.Index(current, planmaxxReminderEnd)
	if start < 0 || end < start {
		return false, nil
	}
	end += len(planmaxxReminderEnd)
	updated := strings.TrimRight(current[:start], "\n") + current[end:]
	updated = strings.TrimLeft(updated, "\n")
	if strings.TrimSpace(updated) != "" {
		updated = strings.TrimRight(updated, "\n") + "\n"
	}
	return writeFileIfChanged(path, []byte(updated))
}

func upsertManagedBlock(path string, desiredBlock string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	currentBytes, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	current := string(currentBytes)
	updated, changed := upsertPlanmaxxReminderBlock(current, desiredBlock)
	if !changed {
		return false, nil
	}
	return writeFilePreservingMode(path, []byte(updated))
}

func upsertPlanmaxxReminderBlock(content string, desiredBlock string) (string, bool) {
	start := strings.Index(content, planmaxxReminderStart)
	end := strings.Index(content, planmaxxReminderEnd)
	if start >= 0 && end >= start {
		end += len(planmaxxReminderEnd)
		if content[start:end] == desiredBlock {
			return content, false
		}
		return content[:start] + desiredBlock + content[end:], true
	}

	trimmed := strings.TrimRight(content, "\n")
	if strings.TrimSpace(trimmed) == "" {
		return desiredBlock + "\n", true
	}
	return trimmed + "\n\n" + desiredBlock + "\n", true
}

func writeFileIfChanged(path string, content []byte) (bool, error) {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, content) {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return writeFilePreservingMode(path, content)
}

func writeFilePreservingMode(path string, content []byte) (bool, error) {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

func removeEmptyDir(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && !isDirectoryNotEmpty(err) {
		return err
	}
	return nil
}

func isDirectoryNotEmpty(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "directory not empty") || strings.Contains(message, "not empty")
}

func expandHomePath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := skillUserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = abs
	}
	return filepath.Clean(path), nil
}
