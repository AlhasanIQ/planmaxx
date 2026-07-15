package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSkillInstallCodexDefaultWritesManagedSkillAndReminder(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"skill", "install", "--target", "codex"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill install failed: %v", err)
	}

	managedPath := filepath.Join(configDir, "planmaxx", "SKILL.md")
	managedBytes, err := os.ReadFile(managedPath)
	if err != nil {
		t.Fatalf("read managed skill: %v", err)
	}
	if !strings.Contains(string(managedBytes), "name: planmaxx") {
		t.Fatalf("managed skill missing frontmatter, got %q", managedBytes)
	}

	installedPath := filepath.Join(home, ".agents", "skills", "planmaxx", "SKILL.md")
	info, err := os.Lstat(installedPath)
	if err != nil {
		t.Fatalf("lstat installed skill: %v", err)
	}
	if runtime.GOOS == "windows" {
		if info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("windows default install should copy instead of symlink")
		}
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("default install should create a symlink on %s", runtime.GOOS)
	}

	agentsPath := filepath.Join(home, ".codex", "AGENTS.md")
	agentsBytes, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read codex AGENTS.md: %v", err)
	}
	agents := string(agentsBytes)
	for _, want := range []string{
		planmaxxReminderStart,
		planmaxxReminderEnd,
		"PlanMaxx",
		"planmaxx skill",
	} {
		if !strings.Contains(agents, want) {
			t.Fatalf("expected AGENTS reminder to contain %q, got %q", want, agents)
		}
	}
}

func TestSkillInstallCopyModeWritesRegularFile(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"skill", "install", "--target", "codex", "--copy"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill install --copy failed: %v", err)
	}

	installedPath := filepath.Join(home, ".agents", "skills", "planmaxx", "SKILL.md")
	info, err := os.Lstat(installedPath)
	if err != nil {
		t.Fatalf("lstat installed skill: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("copy mode should not create a symlink")
	}
	installedBytes, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if string(installedBytes) != planmaxxSkillTestTemplate() {
		t.Fatalf("installed copy did not match template")
	}
}

func TestSkillRemoveDeletesManagedInstallAndReminder(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	installCmd := NewRootCommand(&stdout, &stderr)
	installCmd.SetArgs([]string{"skill", "install", "--target", "codex", "--copy"})
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("skill install failed: %v", err)
	}

	removeCmd := NewRootCommand(&stdout, &stderr)
	removeCmd.SetArgs([]string{"skill", "remove", "--target", "codex"})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("skill remove failed: %v", err)
	}

	installedPath := filepath.Join(home, ".agents", "skills", "planmaxx", "SKILL.md")
	if _, err := os.Lstat(installedPath); !os.IsNotExist(err) {
		t.Fatalf("expected installed skill to be removed, stat err: %v", err)
	}
	agentsBytes, err := os.ReadFile(filepath.Join(home, ".codex", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read codex AGENTS.md: %v", err)
	}
	if strings.Contains(string(agentsBytes), planmaxxReminderStart) {
		t.Fatalf("expected reminder block to be removed, got %q", agentsBytes)
	}
}

func TestSkillRemoveSkipsUserModifiedSkill(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	installedDir := filepath.Join(home, ".agents", "skills", "planmaxx")
	if err := os.MkdirAll(installedDir, 0o755); err != nil {
		t.Fatalf("mkdir installed dir: %v", err)
	}
	installedPath := filepath.Join(installedDir, "SKILL.md")
	if err := os.WriteFile(installedPath, []byte("custom user skill"), 0o644); err != nil {
		t.Fatalf("write custom skill: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"skill", "remove", "--target", "codex"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill remove failed: %v", err)
	}

	kept, err := os.ReadFile(installedPath)
	if err != nil {
		t.Fatalf("custom skill should remain: %v", err)
	}
	if string(kept) != "custom user skill" {
		t.Fatalf("custom skill changed unexpectedly: %q", kept)
	}
	if !strings.Contains(stderr.String(), "Skipped unmanaged skill") {
		t.Fatalf("expected unmanaged skip note, got %q", stderr.String())
	}
}

func TestSkillInstallRepoScoped(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	repoDir := t.TempDir()
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"skill", "install", "--target", "codex", "--repo", repoDir, "--copy"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("repo skill install failed: %v", err)
	}

	repoSkill := filepath.Join(repoDir, ".agents", "skills", "planmaxx", "SKILL.md")
	if _, err := os.Stat(repoSkill); err != nil {
		t.Fatalf("expected repo skill install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "AGENTS.md")); err != nil {
		t.Fatalf("expected repo AGENTS.md reminder: %v", err)
	}
	globalSkill := filepath.Join(home, ".agents", "skills", "planmaxx", "SKILL.md")
	if _, err := os.Stat(globalSkill); !os.IsNotExist(err) {
		t.Fatalf("did not expect global install for repo-scoped command, stat err: %v", err)
	}
}

func TestSkillCommandIsListedInRootHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("root help failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "skill") {
		t.Fatalf("expected root help to list skill command, got %q", stdout.String())
	}
}

func TestSkillHelpKeepsOnlyRepositoryScopeVisible(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"skill", "install", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill install help: %v", err)
	}
	if !strings.Contains(stdout.String(), "--repo") {
		t.Fatalf("expected skill help to contain --repo, got %q", stdout.String())
	}
	for _, hidden := range []string{"--target", "--source", "--copy", "--link"} {
		if strings.Contains(stdout.String(), hidden) {
			t.Fatalf("expected skill help to hide %q, got %q", hidden, stdout.String())
		}
	}
}

func TestREADMEDocumentsSetupAndHowToUseModes(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(repoRootForSkillTest(t), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(readme)
	install := markdownSection(t, content, "## Install", "## Quick Start")
	quickStart := markdownSection(t, content, "## Quick Start", "## Screenshots")

	for _, want := range []string{
		"Automatic Codex Skill",
		"--install-codex-skill",
		"planmaxx skill install",
		"planmaxx skill remove",
		"~/.agents/skills/planmaxx/",
		"planmaxx skill install --repo /path/to/repo",
		"planmaxx skill remove --repo /path/to/repo",
	} {
		if !strings.Contains(install, want) {
			t.Fatalf("expected Install section to mention %q", want)
		}
	}
	for _, want := range []string{
		"planmaxx review path/to/plan.md",
		`tell the agent to "use planmaxx"`,
	} {
		if !strings.Contains(quickStart, want) {
			t.Fatalf("expected Quick Start section to mention %q", want)
		}
	}
	for _, notWant := range []string{
		"--install-codex-skill",
		"planmaxx skill install",
		"planmaxx skill remove",
	} {
		if strings.Contains(quickStart, notWant) {
			t.Fatalf("expected Quick Start to avoid setup detail %q", notWant)
		}
	}
}

func TestInstallerDocumentsOptionalSkillInstall(t *testing.T) {
	installer, err := os.ReadFile(filepath.Join(repoRootForSkillTest(t), "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(installer)
	for _, want := range []string{
		"--install-codex-skill",
		"~/.agents/skills",
		"PLANMAXX_INSTALL_CODEX_SKILL",
		"skill install",
		"${BASE_URL}/SKILL.md",
		"verify_checksum \"${TMPDIR_PLANMAXX}/SKILL.md\" \"$CHECKSUMS\"",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected installer to mention %q", want)
		}
	}
}

func TestRepoSkillMatchesEmbeddedTemplate(t *testing.T) {
	root := repoRootForSkillTest(t)
	repoSkill, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	embeddedSkill, err := os.ReadFile(filepath.Join(root, "internal", "cli", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(repoSkill) != string(embeddedSkill) {
		t.Fatalf("top-level SKILL.md must match internal/cli/SKILL.md")
	}
	for _, want := range []string{"user-scoped `.planmaxx` bundle", "`--local-bundle`", "`<plan-file>.planmaxx` beside the plan"} {
		if !strings.Contains(string(embeddedSkill), want) {
			t.Fatalf("installed skill must document %q", want)
		}
	}
}

func setSkillTestDirs(t *testing.T, home string, configDir string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configDir)
	oldHome := skillUserHomeDir
	oldConfig := skillUserConfigDir
	skillUserHomeDir = func() (string, error) { return home, nil }
	skillUserConfigDir = func() (string, error) { return configDir, nil }
	t.Cleanup(func() {
		skillUserHomeDir = oldHome
		skillUserConfigDir = oldConfig
	})
}

func planmaxxSkillTestTemplate() string {
	return "---\nname: planmaxx\ndescription: Use when a plan is ready for user review.\n---\n\n# PlanMaxx\n"
}

func repoRootForSkillTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func markdownSection(t *testing.T, content string, heading string, nextHeading string) string {
	t.Helper()
	start := strings.Index(content, heading)
	if start < 0 {
		t.Fatalf("missing README heading %q", heading)
	}
	rest := content[start:]
	end := strings.Index(rest, nextHeading)
	if end < 0 {
		t.Fatalf("missing README heading %q after %q", nextHeading, heading)
	}
	return rest[:end]
}
