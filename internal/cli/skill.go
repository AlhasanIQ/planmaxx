package cli

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
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
	skillTargetCodex  = "codex"
	skillTargetClaude = "claude"
	skillTargetGrok   = "grok"
	skillFileName     = "SKILL.md"

	planmaxxReminderStart = "<!-- planmaxx skill reminder:start -->"
	planmaxxReminderEnd   = "<!-- planmaxx skill reminder:end -->"
	planmaxxReminderBody  = "## PlanMaxx Skill\nWhen the PlanMaxx skill is installed, use it whenever an agent-written plan is ready for user review. Check the `planmaxx` skill before proceeding from planning to implementation."

	planmaxxManagedSkillMarker = "<!-- planmaxx-managed-skill -->"

	claudePluginManifest = `{
  "name": "planmaxx",
  "description": "planmaxx-managed:v1 — Review and refine coding-agent plans before implementation.",
  "version": "1.0.0",
  "author": {
    "name": "PlanMaxx"
  }
}
`
	claudeHooksConfig = `{
  "description": "planmaxx-managed:v1 — Expose the active Claude Code session to PlanMaxx.",
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear|compact|fork",
        "hooks": [
          {
            "type": "command",
            "command": "planmaxx claude-session-hook"
          }
        ]
      }
    ]
  }
}
`
	claudeLegacyPluginManifest = `{
  "name": "planmaxx",
  "description": "Review and refine coding-agent plans before implementation.",
  "version": "1.0.0",
  "author": {
    "name": "PlanMaxx"
  }
}
`
	claudeLegacyHooksConfig = `{
  "description": "Expose the active Claude Code session to PlanMaxx.",
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear|compact",
        "hooks": [
          {
            "type": "command",
            "command": "planmaxx claude-session-hook"
          }
        ]
      }
    ]
  }
}
`
)

var legacyManagedSkillHashes = map[string]struct{}{
	"9570c87d0ccb0563a4c246034f752676a4b4fd5231392dd9b7736bc5eb1a6ab2": {},
	"7fab2bb1d2b394df651269b2a565d81a379407d73c9c359c35a044e7e9ba7220": {},
	"721a0c0c9028caa76758509f8c12a8edcb64a716fc463d7a81f8655c798b29a4": {},
	"5e65408969c831171f1434c26da3e4c10ce12648f6adb858139d52cdcbe99998": {},
}

//go:embed SKILL.md
var defaultCodexSkillTemplate []byte

//go:embed SKILL.claude.md
var defaultClaudeSkillTemplate []byte

//go:embed SKILL.grok.md
var defaultGrokSkillTemplate []byte

var (
	skillTemplatesEmbedded = map[string][]byte{
		skillTargetCodex:  append([]byte(nil), defaultCodexSkillTemplate...),
		skillTargetClaude: append([]byte(nil), defaultClaudeSkillTemplate...),
		skillTargetGrok:   append([]byte(nil), defaultGrokSkillTemplate...),
	}
	skillUserHomeDir   = os.UserHomeDir
	skillUserConfigDir = os.UserConfigDir
)

// SetEmbeddedSkillTemplate replaces the embedded skill template.
// Tests use this to keep install/remove behavior deterministic.
func SetEmbeddedSkillTemplate(b []byte) {
	for _, target := range []string{skillTargetCodex, skillTargetClaude, skillTargetGrok} {
		skillTemplatesEmbedded[target] = append([]byte(nil), b...)
	}
}

func newSkillCommand(stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill <install|remove>",
		Short: "Install or remove an optional PlanMaxx agent skill",
	}
	cmd.AddCommand(newSkillInstallCommand(stderr))
	cmd.AddCommand(newSkillRemoveCommand(stderr))
	return cmd
}

func newSkillInstallCommand(stderr io.Writer) *cobra.Command {
	opts := skillInstallOptions{target: skillTargetCodex}
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install PlanMaxx as an optional agent skill",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillInstall(opts, stderr)
		},
	}
	cmd.Flags().StringVar(&opts.target, "target", opts.target, "skill target: codex, claude, or grok")
	cmd.Flags().StringVar(&opts.repo, "repo", "", "install inside this repository instead of the user-level agent directory")
	cmd.Flags().StringVar(&opts.source, "source", "", "local SKILL.md source path")
	cmd.Flags().BoolVar(&opts.copyMode, "copy", false, "copy SKILL.md instead of symlinking")
	cmd.Flags().BoolVar(&opts.linkMode, "link", false, "symlink SKILL.md instead of copying")
	for _, name := range []string{"source", "copy", "link"} {
		_ = cmd.Flags().MarkHidden(name)
	}
	return cmd
}

func newSkillRemoveCommand(stderr io.Writer) *cobra.Command {
	opts := skillRemoveOptions{target: skillTargetCodex}
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a PlanMaxx agent skill installed by PlanMaxx",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillRemove(opts, stderr)
		},
	}
	cmd.Flags().StringVar(&opts.target, "target", opts.target, "skill target: codex, claude, or grok")
	cmd.Flags().StringVar(&opts.repo, "repo", "", "remove from this repository instead of the user-level agent directory")
	cmd.Flags().BoolVar(&opts.keepReminder, "keep-reminder", false, "leave the PlanMaxx reminder block in AGENTS.md")
	for _, name := range []string{"keep-reminder"} {
		_ = cmd.Flags().MarkHidden(name)
	}
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

type managedSkillFile struct {
	path          string
	content       []byte
	legacyContent [][]byte
}

func runSkillInstall(opts skillInstallOptions, stderr io.Writer) error {
	target, err := normalizeSkillTarget(opts.target)
	if err != nil {
		return err
	}
	if opts.copyMode && opts.linkMode {
		return errors.New("--copy and --link cannot be used together")
	}
	if (target == skillTargetClaude || target == skillTargetGrok) && opts.linkMode {
		return fmt.Errorf("%s skill installs use copy mode; --link is not supported", skillTargetDisplayName(target))
	}
	if (target == skillTargetClaude || target == skillTargetGrok) && strings.TrimSpace(opts.source) != "" {
		return fmt.Errorf("%s skill installs do not support custom --source files", skillTargetDisplayName(target))
	}
	sourceBytes, sourcePath, sourceLabel, err := loadSkillSource(opts.source, target)
	if err != nil {
		return err
	}
	repoRoot, err := resolveSkillRepoRoot(opts.repo)
	if err != nil {
		return err
	}
	destination, reminderFile, extraFiles, err := resolveSkillPaths(target, repoRoot)
	if err != nil {
		return err
	}
	if err := preflightSkillFile(filepath.Join(destination, skillFileName), sourceBytes, sourcePath); err != nil {
		return err
	}
	var legacyClaudeSkill string
	var legacyClaudeFiles []managedSkillFile
	if target == skillTargetClaude {
		legacyClaudeSkill = filepath.Join(destination, "skills", "planmaxx", skillFileName)
		if err := preflightSkillFile(legacyClaudeSkill, sourceBytes, sourcePath); err != nil {
			return err
		}
		legacyClaudeFiles = claudeLegacyPluginFiles(destination)
		if err := preflightManagedFiles(legacyClaudeFiles); err != nil {
			return err
		}
	}
	if err := preflightManagedFiles(extraFiles); err != nil {
		return err
	}

	if target == skillTargetClaude {
		var managedSources []string
		for _, managedTarget := range []string{skillTargetCodex, skillTargetClaude, skillTargetGrok} {
			managedSource, err := defaultManagedSkillSourcePath(managedTarget)
			if err != nil {
				return err
			}
			managedSources = append(managedSources, managedSource)
		}
		removed, _, err := removeManagedSkillFile(legacyClaudeSkill, managedSources...)
		if err != nil {
			return fmt.Errorf("remove legacy Claude plugin skill %s: %w", legacyClaudeSkill, err)
		}
		if removed {
			fmt.Fprintf(stderr, "Removed legacy Claude plugin skill %s\n", legacyClaudeSkill)
		}
		for _, file := range legacyClaudeFiles {
			removed, _, err := removeManagedFile(file)
			if err != nil {
				return fmt.Errorf("remove legacy Claude plugin file %s: %w", file.path, err)
			}
			if removed {
				fmt.Fprintf(stderr, "Removed legacy Claude plugin file %s\n", file.path)
			}
			_ = removeEmptyDir(filepath.Dir(file.path))
		}
		_ = removeEmptyDir(filepath.Dir(legacyClaudeSkill))
		_ = removeEmptyDir(filepath.Join(destination, "skills"))
	}

	linkMode := opts.linkMode
	if !opts.linkMode && !opts.copyMode {
		linkMode = runtime.GOOS != "windows"
	}
	if opts.copyMode || target == skillTargetClaude || target == skillTargetGrok {
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
	fmt.Fprintf(stderr, "Installing PlanMaxx %s skill (%s mode, %s) from %s\n", target, mode, scope, sourceLabel)
	installedPath, err := installSkillFile(destination, sourceBytes, sourcePath, linkMode)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "Installed %s\n", installedPath)

	for _, file := range extraFiles {
		changed, err := writeManagedFile(file)
		if err != nil {
			return err
		}
		if changed {
			fmt.Fprintf(stderr, "Installed %s\n", file.path)
		}
	}

	if reminderFile != "" {
		changed, err := ensurePlanmaxxReminder(reminderFile)
		if err != nil {
			return fmt.Errorf("update reminder in %s: %w", reminderFile, err)
		}
		if changed {
			fmt.Fprintf(stderr, "Updated PlanMaxx reminder in %s\n", reminderFile)
		} else {
			fmt.Fprintf(stderr, "PlanMaxx reminder already present in %s\n", reminderFile)
		}
	}
	return nil
}

func runSkillRemove(opts skillRemoveOptions, stderr io.Writer) error {
	target, err := normalizeSkillTarget(opts.target)
	if err != nil {
		return err
	}
	repoRoot, err := resolveSkillRepoRoot(opts.repo)
	if err != nil {
		return err
	}
	destination, reminderFile, extraFiles, err := resolveSkillPaths(target, repoRoot)
	if err != nil {
		return err
	}
	managedSource, err := defaultManagedSkillSourcePath(target)
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
	if target == skillTargetClaude {
		legacyPath := filepath.Join(destination, "skills", "planmaxx", skillFileName)
		legacyRemoved, legacySkipped, err := removeManagedSkillFile(legacyPath, managedSource)
		if err != nil {
			return err
		}
		switch {
		case legacyRemoved:
			fmt.Fprintf(stderr, "Removed legacy Claude plugin skill %s\n", legacyPath)
		case legacySkipped:
			fmt.Fprintf(stderr, "Skipped unmanaged skill at %s\n", legacyPath)
		}
		for _, file := range claudeLegacyPluginFiles(destination) {
			extraRemoved, extraSkipped, err := removeManagedFile(file)
			if err != nil {
				return err
			}
			switch {
			case extraRemoved:
				fmt.Fprintf(stderr, "Removed legacy Claude plugin file %s\n", file.path)
			case extraSkipped:
				fmt.Fprintf(stderr, "Skipped unmanaged file at %s\n", file.path)
			}
			_ = removeEmptyDir(filepath.Dir(file.path))
		}
	}

	for _, file := range extraFiles {
		extraRemoved, extraSkipped, err := removeManagedFile(file)
		if err != nil {
			return err
		}
		switch {
		case extraRemoved:
			fmt.Fprintf(stderr, "Removed %s\n", file.path)
		case extraSkipped:
			fmt.Fprintf(stderr, "Skipped unmanaged file at %s\n", file.path)
		}
		_ = removeEmptyDir(filepath.Dir(file.path))
	}
	if target == skillTargetClaude {
		_ = removeEmptyDir(filepath.Join(destination, "skills", "planmaxx"))
		_ = removeEmptyDir(filepath.Join(destination, "skills"))
	}
	_ = removeEmptyDir(destination)

	if !opts.keepReminder && reminderFile != "" {
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
	_, err := normalizeSkillTarget(raw)
	return err
}

func normalizeSkillTarget(raw string) (string, error) {
	target := strings.ToLower(strings.TrimSpace(raw))
	switch target {
	case "", skillTargetCodex:
		return skillTargetCodex, nil
	case skillTargetClaude:
		return skillTargetClaude, nil
	case skillTargetGrok:
		return skillTargetGrok, nil
	default:
		return "", fmt.Errorf("target must be codex, claude, or grok")
	}
}

func loadSkillSource(sourceRaw string, target string) ([]byte, string, string, error) {
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

	managedPath, err := defaultManagedSkillSourcePath(target)
	if err != nil {
		return nil, "", "", err
	}
	template := skillTemplatesEmbedded[target]
	if len(bytes.TrimSpace(template)) == 0 {
		return nil, "", "", fmt.Errorf("embedded skill template is empty")
	}
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
		return nil, "", "", err
	}

	existing, readErr := readManagedSkillSource(managedPath)
	if readErr == nil && bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(template)) {
		return existing, managedPath, managedPath, nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, "", "", readErr
	}

	status := "seeded from embedded template"
	if readErr == nil {
		status = "updated from embedded template"
	}
	if err := writeFileAtomic(managedPath, template, 0o644); err != nil {
		return nil, "", "", err
	}
	return template, managedPath, fmt.Sprintf("%s (%s)", managedPath, status), nil
}

func defaultManagedSkillSourcePath(target string) (string, error) {
	configDir, err := skillUserConfigDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(configDir) == "" {
		return "", fmt.Errorf("user config directory is empty")
	}
	if target == skillTargetCodex {
		return filepath.Join(configDir, "planmaxx", skillFileName), nil
	}
	return filepath.Join(configDir, "planmaxx", "skills", target, skillFileName), nil
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

func resolveClaudeSkillPaths(repoRoot string) (string, error) {
	if repoRoot != "" {
		return filepath.Join(repoRoot, ".claude", "skills", "planmaxx"), nil
	}

	if configured := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); configured != "" {
		configDir, err := expandHomePath(configured)
		if err != nil {
			return "", err
		}
		return filepath.Join(configDir, "skills", "planmaxx"), nil
	}

	home, err := skillUserHomeDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("home directory is empty")
	}
	return filepath.Join(home, ".claude", "skills", "planmaxx"), nil
}

func resolveGrokSkillPaths(repoRoot string) (string, error) {
	if repoRoot != "" {
		return filepath.Join(repoRoot, ".grok", "skills", "planmaxx"), nil
	}

	if configured := strings.TrimSpace(os.Getenv("GROK_HOME")); configured != "" {
		configDir, err := expandHomePath(configured)
		if err != nil {
			return "", err
		}
		return filepath.Join(configDir, "skills", "planmaxx"), nil
	}

	home, err := skillUserHomeDir()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("home directory is empty")
	}
	return filepath.Join(home, ".grok", "skills", "planmaxx"), nil
}

func resolveSkillPaths(target string, repoRoot string) (string, string, []managedSkillFile, error) {
	switch target {
	case skillTargetCodex:
		destination, reminderFile, err := resolveCodexSkillPaths(repoRoot)
		return destination, reminderFile, nil, err
	case skillTargetClaude:
		destination, err := resolveClaudeSkillPaths(repoRoot)
		if err != nil {
			return "", "", nil, err
		}
		return destination, "", nil, nil
	case skillTargetGrok:
		destination, err := resolveGrokSkillPaths(repoRoot)
		if err != nil {
			return "", "", nil, err
		}
		return destination, "", nil, nil
	default:
		return "", "", nil, fmt.Errorf("target must be codex, claude, or grok")
	}
}

func claudeLegacyPluginFiles(destination string) []managedSkillFile {
	return []managedSkillFile{
		{
			path:          filepath.Join(destination, ".claude-plugin", "plugin.json"),
			content:       []byte(claudePluginManifest),
			legacyContent: [][]byte{[]byte(claudeLegacyPluginManifest)},
		},
		{
			path:          filepath.Join(destination, "hooks", "hooks.json"),
			content:       []byte(claudeHooksConfig),
			legacyContent: [][]byte{[]byte(claudeLegacyHooksConfig)},
		},
	}
}

func preflightSkillFile(path string, desired []byte, sourcePath string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		if sourcePath != "" && isManagedSkillLink(target, path, sourcePath) {
			return nil
		}
		for _, managedTarget := range []string{skillTargetCodex, skillTargetClaude, skillTargetGrok} {
			managedSource, err := defaultManagedSkillSourcePath(managedTarget)
			if err == nil && isManagedSkillLink(target, path, managedSource) {
				return nil
			}
		}
		return fmt.Errorf("refusing to overwrite unmanaged skill: %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to overwrite unmanaged skill: %s", path)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if isManagedSkillContent(current) || bytes.Equal(current, desired) {
		return nil
	}
	return fmt.Errorf("refusing to overwrite unmanaged skill: %s", path)
}

func preflightManagedFiles(files []managedSkillFile) error {
	for _, file := range files {
		info, err := os.Lstat(file.path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to overwrite unmanaged file: %s", file.path)
		}
		current, err := os.ReadFile(file.path)
		if err != nil {
			return err
		}
		if !isManagedAuxiliaryContent(file, current) {
			return fmt.Errorf("refusing to overwrite unmanaged file: %s", file.path)
		}
	}
	return nil
}

func writeManagedFile(file managedSkillFile) (bool, error) {
	changed, err := writeFileIfChanged(file.path, file.content)
	if err != nil {
		return false, fmt.Errorf("install managed file %s: %w", file.path, err)
	}
	return changed, nil
}

func removeManagedFile(file managedSkillFile) (removed bool, skipped bool, err error) {
	info, err := os.Lstat(file.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, false, nil
		}
		return false, false, err
	}
	if !info.Mode().IsRegular() {
		return false, true, nil
	}
	current, err := os.ReadFile(file.path)
	if err != nil {
		return false, false, err
	}
	if !isManagedAuxiliaryContent(file, current) {
		return false, true, nil
	}
	return true, false, os.Remove(file.path)
}

func isManagedAuxiliaryContent(file managedSkillFile, current []byte) bool {
	if bytes.Equal(current, file.content) {
		return true
	}
	for _, legacy := range file.legacyContent {
		if bytes.Equal(current, legacy) {
			return true
		}
	}
	return false
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
		tmp, err := os.CreateTemp(destinationDir, ".planmaxx-skill-link-*")
		if err != nil {
			return "", err
		}
		tmpPath := tmp.Name()
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return "", err
		}
		if err := os.Remove(tmpPath); err != nil {
			return "", err
		}
		if err := os.Symlink(absSourcePath, tmpPath); err != nil {
			return "", err
		}
		if err := os.Rename(tmpPath, targetFile); err != nil {
			_ = os.Remove(tmpPath)
			return "", err
		}
		return targetFile, nil
	}

	if err := writeFileAtomic(targetFile, sourceBytes, 0o644); err != nil {
		return "", err
	}
	return targetFile, nil
}

func readManagedSkillSource(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing unmanaged PlanMaxx skill source: %s", path)
	}
	file, err := openSkillFileNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("PlanMaxx skill source changed while reading: %s", path)
	}
	return io.ReadAll(io.LimitReader(file, 1<<20))
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	removeTemp = false
	return nil
}

func removeManagedSkillFile(targetFile string, managedSources ...string) (removed bool, skipped bool, err error) {
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
		for _, managedSource := range managedSources {
			if isManagedSkillLink(linkTarget, targetFile, managedSource) {
				return true, false, os.Remove(targetFile)
			}
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

func isManagedSkillLink(linkTarget, linkPath, managedSource string) bool {
	if !filepath.IsAbs(linkTarget) {
		linkTarget = filepath.Join(filepath.Dir(linkPath), linkTarget)
	}
	absTarget, targetErr := filepath.Abs(linkTarget)
	absManaged, managedErr := filepath.Abs(managedSource)
	if targetErr != nil || managedErr != nil {
		return false
	}
	return filepath.Clean(absTarget) == filepath.Clean(absManaged)
}

func isManagedSkillContent(content []byte) bool {
	for _, template := range skillTemplatesEmbedded {
		if bytes.Equal(bytes.TrimSpace(content), bytes.TrimSpace(template)) {
			return true
		}
	}
	digest := sha256.Sum256(bytes.TrimSpace(content))
	_, ok := legacyManagedSkillHashes[hex.EncodeToString(digest[:])]
	return ok
}

func skillTargetDisplayName(target string) string {
	switch target {
	case skillTargetClaude:
		return "Claude Code"
	case skillTargetGrok:
		return "Grok Build"
	default:
		return "Codex"
	}
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
	writePath, err := resolveFileWritePath(path)
	if err != nil {
		return false, err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(writePath); err == nil {
		mode = info.Mode().Perm()
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(writePath), 0o755); err != nil {
		return false, err
	}
	tmp := writePath + ".tmp"
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, writePath); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

func resolveFileWritePath(path string) (string, error) {
	current := filepath.Clean(path)
	seen := make(map[string]struct{})
	for {
		if _, exists := seen[current]; exists {
			return "", fmt.Errorf("symlink cycle while resolving %s", path)
		}
		seen[current] = struct{}{}

		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return current, nil
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return current, nil
		}

		target, err := os.Readlink(current)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}
		current = filepath.Clean(target)
	}
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
