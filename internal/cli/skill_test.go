package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSkillInstallGrokWritesNativeInvocationScopedSkill(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	var stderr bytes.Buffer
	cmd := NewRootCommand(&bytes.Buffer{}, &stderr)
	cmd.SetArgs([]string{"skill", "install", "--target", "grok"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Grok skill install failed: %v", err)
	}

	skillPath := filepath.Join(home, ".grok", "skills", "planmaxx", "SKILL.md")
	got, err := os.ReadFile(skillPath)
	if err != nil || string(got) != planmaxxSkillTestTemplate() {
		t.Fatalf("expected native Grok skill: content=%q err=%v", got, err)
	}
	if info, err := os.Lstat(skillPath); err != nil {
		t.Fatal(err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("Grok skill must be installed as a regular file")
	}
	if !strings.Contains(stderr.String(), "PlanMaxx grok skill (copy mode") {
		t.Fatalf("unexpected install status: %s", stderr.String())
	}
}

func TestSkillInstallRejectsSymlinkedManagedSourceWithoutTouchingTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated privileges on Windows")
	}
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("do not overwrite\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	managedPath, err := defaultManagedSkillSourcePath(skillTargetGrok)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, managedPath); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "grok"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unmanaged PlanMaxx skill source") {
		t.Fatalf("expected symlinked managed-source rejection, got %v", err)
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "do not overwrite\n" {
		t.Fatalf("managed-source symlink target changed: content=%q err=%v", got, err)
	}
}

func TestSkillCopyIgnoresPredictableTemporarySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated privileges on Windows")
	}
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	destination := filepath.Join(home, ".grok", "skills", "planmaxx")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("do not overwrite\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(destination, "SKILL.md.tmp")); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "grok"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "do not overwrite\n" {
		t.Fatalf("predictable temporary symlink target changed: content=%q err=%v", got, err)
	}
	target := filepath.Join(destination, skillFileName)
	if info, err := os.Lstat(target); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("installed Grok skill is not a regular file: info=%v err=%v", info, err)
	}
}

func TestSkillInstallGrokRepoScopedAndUsesGrokHome(t *testing.T) {
	t.Run("repository", func(t *testing.T) {
		home := t.TempDir()
		configDir := filepath.Join(t.TempDir(), "config")
		repoDir := t.TempDir()
		setSkillTestDirs(t, home, configDir)
		SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

		cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
		cmd.SetArgs([]string{"skill", "install", "--target", "grok", "--repo", repoDir})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		skillPath := filepath.Join(repoDir, ".grok", "skills", "planmaxx", "SKILL.md")
		if got, err := os.ReadFile(skillPath); err != nil || string(got) != planmaxxSkillTestTemplate() {
			t.Fatalf("expected repo Grok skill: content=%q err=%v", got, err)
		}
		if _, err := os.Stat(filepath.Join(home, ".grok", "skills", "planmaxx", "SKILL.md")); !os.IsNotExist(err) {
			t.Fatalf("global Grok skill should remain untouched: %v", err)
		}
	})

	t.Run("GROK_HOME", func(t *testing.T) {
		home := t.TempDir()
		configDir := filepath.Join(t.TempDir(), "config")
		grokHome := filepath.Join(t.TempDir(), "custom-grok")
		setSkillTestDirs(t, home, configDir)
		t.Setenv("GROK_HOME", grokHome)
		SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

		cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
		cmd.SetArgs([]string{"skill", "install", "--target", "grok"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(grokHome, "skills", "planmaxx", "SKILL.md")); err != nil {
			t.Fatalf("expected skill under GROK_HOME: %v", err)
		}
		if _, err := os.Stat(filepath.Join(home, ".grok", "skills", "planmaxx", "SKILL.md")); !os.IsNotExist(err) {
			t.Fatalf("default Grok home should remain untouched: %v", err)
		}
	})
}

func TestSkillRemoveGrokIsIdempotentAndPreservesUnmanagedFiles(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	install := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	install.SetArgs([]string{"skill", "install", "--target", "grok"})
	if err := install.Execute(); err != nil {
		t.Fatal(err)
	}
	remove := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	remove.SetArgs([]string{"skill", "remove", "--target", "grok"})
	if err := remove.Execute(); err != nil {
		t.Fatal(err)
	}
	secondStderr := &bytes.Buffer{}
	removeAgain := NewRootCommand(&bytes.Buffer{}, secondStderr)
	removeAgain.SetArgs([]string{"skill", "remove", "--target", "grok"})
	if err := removeAgain.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(secondStderr.String(), "not found") {
		t.Fatalf("expected idempotent removal status: %s", secondStderr.String())
	}

	skillPath := filepath.Join(home, ".grok", "skills", "planmaxx", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("# my custom skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unmanagedStderr := &bytes.Buffer{}
	removeUnmanaged := NewRootCommand(&bytes.Buffer{}, unmanagedStderr)
	removeUnmanaged.SetArgs([]string{"skill", "remove", "--target", "grok"})
	if err := removeUnmanaged.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(skillPath); err != nil || string(got) != "# my custom skill\n" {
		t.Fatalf("unmanaged skill changed: content=%q err=%v", got, err)
	}
	if !strings.Contains(unmanagedStderr.String(), "Skipped unmanaged skill") {
		t.Fatalf("missing unmanaged warning: %s", unmanagedStderr.String())
	}
}

func TestSkillInstallGrokRejectsLinkMode(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "grok", "--link"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "Grok Build skill installs use copy mode") {
		t.Fatalf("expected Grok link rejection, got %v", err)
	}
}

func TestSkillInstallGrokRejectsCustomSource(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	source := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(source, []byte(planmaxxSkillTestTemplate()), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "grok", "--source", source})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "do not support custom --source") {
		t.Fatalf("expected custom source rejection, got %v", err)
	}
}

func TestSkillInstallAndRemovePreserveCustomizedManagedMarker(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))
	skillPath := filepath.Join(home, ".grok", "skills", "planmaxx", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	custom := []byte("---\nname: custom\n---\n\n<!-- planmaxx-managed-skill -->\nCustomized.\n")
	if err := os.WriteFile(skillPath, custom, 0o644); err != nil {
		t.Fatal(err)
	}

	install := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	install.SetArgs([]string{"skill", "install", "--target", "grok"})
	if err := install.Execute(); err == nil || !strings.Contains(err.Error(), "refusing to overwrite unmanaged skill") {
		t.Fatalf("expected customized marker file to block install, got %v", err)
	}

	var stderr bytes.Buffer
	remove := NewRootCommand(&bytes.Buffer{}, &stderr)
	remove.SetArgs([]string{"skill", "remove", "--target", "grok"})
	if err := remove.Execute(); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(skillPath); err != nil || !bytes.Equal(content, custom) {
		t.Fatalf("customized marker file changed: content=%q err=%v", content, err)
	}
	if !strings.Contains(stderr.String(), "Skipped unmanaged skill") {
		t.Fatalf("expected unmanaged warning, got %s", stderr.String())
	}
}

func TestSkillInstallGrokUsesProductionTemplate(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	previous := skillTemplatesEmbedded
	skillTemplatesEmbedded = map[string][]byte{
		skillTargetCodex:  append([]byte(nil), defaultCodexSkillTemplate...),
		skillTargetClaude: append([]byte(nil), defaultClaudeSkillTemplate...),
		skillTargetGrok:   append([]byte(nil), defaultGrokSkillTemplate...),
	}
	t.Cleanup(func() { skillTemplatesEmbedded = previous })

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "grok"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(home, ".grok", "skills", "planmaxx", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(content, defaultGrokSkillTemplate) {
		t.Fatal("Grok target did not install the production Grok template")
	}
	if !strings.Contains(string(content), "planmaxx review --grok-session-id ${SESSION_ID}") ||
		strings.Contains(string(content), "--claude-session-id") {
		t.Fatalf("installed production Grok invocation is incorrect:\n%s", content)
	}
}

func TestSkillInstallClaudeWritesPlainInvocationScopedSkill(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"skill", "install", "--target", "claude"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Claude skill install failed: %v", err)
	}

	installDir := filepath.Join(home, ".claude", "skills", "planmaxx")
	skillPath := filepath.Join(installDir, "SKILL.md")
	skillBytes, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read Claude skill: %v", err)
	}
	if string(skillBytes) != planmaxxSkillTestTemplate() {
		t.Fatalf("installed Claude skill did not match template")
	}
	skillInfo, err := os.Lstat(skillPath)
	if err != nil {
		t.Fatalf("lstat Claude skill: %v", err)
	}
	if skillInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatal("Claude skill must be installed as a regular file")
	}

	for _, relative := range []string{
		filepath.Join(".claude-plugin", "plugin.json"),
		filepath.Join("hooks", "hooks.json"),
		filepath.Join("skills", "planmaxx", "SKILL.md"),
	} {
		if _, err := os.Lstat(filepath.Join(installDir, relative)); !os.IsNotExist(err) {
			t.Fatalf("plain Claude skill unexpectedly installed plugin component %s: %v", relative, err)
		}
	}
}

func TestSkillInstallClaudeRepoScoped(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	repoDir := t.TempDir()
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"skill", "install", "--target", "claude", "--repo", repoDir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("repo Claude skill install failed: %v", err)
	}

	repoInstallDir := filepath.Join(repoDir, ".claude", "skills", "planmaxx")
	repoSkill := filepath.Join(repoInstallDir, "SKILL.md")
	if got, err := os.ReadFile(repoSkill); err != nil || string(got) != planmaxxSkillTestTemplate() {
		t.Fatalf("expected repo Claude skill at documented path: content=%q err=%v", got, err)
	}
	if info, err := os.Lstat(repoSkill); err != nil {
		t.Fatalf("lstat repo Claude skill: %v", err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("repo Claude skill must be a regular file")
	}
	for _, relativePath := range []string{
		filepath.Join(".claude-plugin", "plugin.json"),
		filepath.Join("hooks", "hooks.json"),
		filepath.Join("skills", "planmaxx", "SKILL.md"),
	} {
		if _, err := os.Lstat(filepath.Join(repoInstallDir, relativePath)); !os.IsNotExist(err) {
			t.Fatalf("repo Claude install unexpectedly created %s: %v", relativePath, err)
		}
	}
	globalSkill := filepath.Join(home, ".claude", "skills", "planmaxx", "SKILL.md")
	if _, err := os.Stat(globalSkill); !os.IsNotExist(err) {
		t.Fatalf("did not expect global Claude install for repo-scoped command, stat err: %v", err)
	}
}

func TestSkillInstallClaudeUsesConfiguredClaudeDirectory(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	claudeConfigDir := filepath.Join(t.TempDir(), "claude-config")
	setSkillTestDirs(t, home, configDir)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeConfigDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Claude skill install failed: %v", err)
	}

	configuredSkill := filepath.Join(claudeConfigDir, "skills", "planmaxx", "SKILL.md")
	if _, err := os.Stat(configuredSkill); err != nil {
		t.Fatalf("expected skill under CLAUDE_CONFIG_DIR: %v", err)
	}
	if info, err := os.Lstat(configuredSkill); err != nil {
		t.Fatalf("lstat configured Claude skill: %v", err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("configured Claude skill must be a regular file")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "planmaxx", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("default Claude directory should remain untouched, stat err: %v", err)
	}
}

func TestSkillInstallMigratesManagedClaudePluginToPlainSkill(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	installDir := filepath.Join(home, ".claude", "skills", "planmaxx")
	legacySkill := filepath.Join(installDir, "skills", "planmaxx", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(legacySkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(installDir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(installDir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacySkill, []byte(planmaxxSkillTestTemplate()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, ".claude-plugin", "plugin.json"), []byte(claudePluginManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "hooks", "hooks.json"), []byte(claudeHooksConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	installCmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	installCmd.SetArgs([]string{"skill", "install", "--target", "claude", "--copy"})
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("upgrade previous Claude plugin: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "SKILL.md")); err != nil || string(got) != planmaxxSkillTestTemplate() {
		t.Fatalf("plain Claude skill was not installed: content=%q err=%v", got, err)
	}
	for _, legacyPath := range []string{
		legacySkill,
		filepath.Join(installDir, ".claude-plugin", "plugin.json"),
		filepath.Join(installDir, "hooks", "hooks.json"),
	} {
		if _, err := os.Lstat(legacyPath); !os.IsNotExist(err) {
			t.Fatalf("managed legacy plugin artifact should be removed: %s: %v", legacyPath, err)
		}
	}

	// Re-running after migration must be idempotent.
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("repeat plain Claude install: %v", err)
	}
	removeCmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	removeCmd.SetArgs([]string{"skill", "remove", "--target", "claude"})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("remove migrated Claude skill: %v", err)
	}
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Fatalf("migrated Claude skill should be removed, stat err: %v", err)
	}
}

func TestSkillInstallMigratesRelativeLegacyLinkToCodexManagedSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation commonly requires elevated privileges on Windows")
	}
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	managedCodex, err := defaultManagedSkillSourcePath(skillTargetCodex)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(managedCodex), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedCodex, []byte(planmaxxSkillTestTemplate()), 0o644); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(home, ".claude", "skills", "planmaxx")
	legacySkill := filepath.Join(installDir, "skills", "planmaxx", skillFileName)
	if err := os.MkdirAll(filepath.Dir(legacySkill), 0o755); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(filepath.Dir(legacySkill), managedCodex)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relative, legacySkill); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("migrate relative legacy link: %v", err)
	}
	if _, err := os.Lstat(legacySkill); !os.IsNotExist(err) {
		t.Fatalf("relative legacy link remains after migration: %v", err)
	}
	if info, err := os.Lstat(filepath.Join(installDir, skillFileName)); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("plain Claude skill missing after migration: info=%v err=%v", info, err)
	}
}

func TestSkillInstallMigratesLegacyRootPluginSchema(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	installDir := filepath.Join(home, ".claude", "skills", "planmaxx")
	for _, dir := range []string{filepath.Join(installDir, ".claude-plugin"), filepath.Join(installDir, "hooks")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(installDir, "SKILL.md"), []byte(planmaxxSkillTestTemplate()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, ".claude-plugin", "plugin.json"), []byte(claudeLegacyPluginManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "hooks", "hooks.json"), []byte(claudeLegacyHooksConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("migrate legacy root plugin: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "SKILL.md")); err != nil || string(got) != planmaxxSkillTestTemplate() {
		t.Fatalf("plain skill changed unexpectedly: content=%q err=%v", got, err)
	}
	for _, relative := range []string{filepath.Join(".claude-plugin", "plugin.json"), filepath.Join("hooks", "hooks.json")} {
		if _, err := os.Lstat(filepath.Join(installDir, relative)); !os.IsNotExist(err) {
			t.Fatalf("legacy root plugin artifact should be removed: %s: %v", relative, err)
		}
	}
}

func TestSkillInstallClaudeRecoversInterruptedMigration(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	installDir := filepath.Join(home, ".claude", "skills", "planmaxx")
	paths := map[string]string{
		filepath.Join(installDir, "SKILL.md"):                       planmaxxSkillTestTemplate(),
		filepath.Join(installDir, "skills", "planmaxx", "SKILL.md"): planmaxxSkillTestTemplate(),
		filepath.Join(installDir, ".claude-plugin", "plugin.json"):  claudePluginManifest,
		filepath.Join(installDir, "hooks", "hooks.json"):            claudeHooksConfig,
	}
	for path, content := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "claude"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("recover interrupted Claude migration: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(installDir, "SKILL.md")); err != nil || string(got) != planmaxxSkillTestTemplate() {
		t.Fatalf("plain skill not preserved: content=%q err=%v", got, err)
	}
	for path := range paths {
		if path == filepath.Join(installDir, "SKILL.md") {
			continue
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("stale managed artifact should be removed: %s: %v", path, err)
		}
	}
}

func TestSkillRemoveClaudeDeletesManagedPlainSkill(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	installCmd := NewRootCommand(&stdout, &stderr)
	installCmd.SetArgs([]string{"skill", "install", "--target", "claude", "--copy"})
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Claude skill install failed: %v", err)
	}

	removeCmd := NewRootCommand(&stdout, &stderr)
	removeCmd.SetArgs([]string{"skill", "remove", "--target", "claude"})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("Claude skill remove failed: %v", err)
	}

	installDir := filepath.Join(home, ".claude", "skills", "planmaxx")
	if _, err := os.Lstat(installDir); !os.IsNotExist(err) {
		t.Fatalf("expected managed Claude skill directory to be removed, stat err: %v", err)
	}
}

func TestSkillRemoveClaudePreservesUnmanagedLegacyHook(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	installCmd := NewRootCommand(&stdout, &stderr)
	installCmd.SetArgs([]string{"skill", "install", "--target", "claude", "--copy"})
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Claude skill install failed: %v", err)
	}

	hookPath := filepath.Join(home, ".claude", "skills", "planmaxx", "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatalf("create legacy hook directory: %v", err)
	}
	customHook := []byte("{\"custom\":true}\n")
	if err := os.WriteFile(hookPath, customHook, 0o644); err != nil {
		t.Fatalf("modify Claude hook: %v", err)
	}

	removeCmd := NewRootCommand(&stdout, &stderr)
	removeCmd.SetArgs([]string{"skill", "remove", "--target", "claude"})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("Claude skill remove failed: %v", err)
	}

	kept, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("modified Claude hook should remain: %v", err)
	}
	if !bytes.Equal(kept, customHook) {
		t.Fatalf("modified Claude hook changed unexpectedly: %q", kept)
	}
	if !strings.Contains(stderr.String(), "Skipped unmanaged file") {
		t.Fatalf("expected unmanaged hook skip note, got %q", stderr.String())
	}
}

func TestSkillRemoveClaudePreservesCustomizedManagedLegacyHook(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	installDir := filepath.Join(home, ".claude", "skills", "planmaxx")
	hookPath := filepath.Join(installDir, "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	customized := []byte(strings.Replace(claudeHooksConfig, `"matcher": "startup|resume|clear|compact|fork"`, `"matcher": "startup"`, 1))
	if err := os.WriteFile(hookPath, customized, 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	removeCmd := NewRootCommand(&bytes.Buffer{}, &stderr)
	removeCmd.SetArgs([]string{"skill", "remove", "--target", "claude"})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("Claude skill remove failed: %v", err)
	}
	if got, err := os.ReadFile(hookPath); err != nil || !bytes.Equal(got, customized) {
		t.Fatalf("customized managed hook changed: content=%q err=%v", got, err)
	}
	if !strings.Contains(stderr.String(), "Skipped unmanaged file") {
		t.Fatalf("expected customized hook skip note, got %q", stderr.String())
	}
}

func TestSkillRemoveClaudeKeepsModifiedPlainSkillAndRemovesManagedLegacyArtifacts(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	var stderr bytes.Buffer
	installCmd := NewRootCommand(&bytes.Buffer{}, &stderr)
	installCmd.SetArgs([]string{"skill", "install", "--target", "claude", "--copy"})
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("Claude skill install failed: %v", err)
	}

	installDir := filepath.Join(home, ".claude", "skills", "planmaxx")
	skillPath := filepath.Join(installDir, "SKILL.md")
	const customSkill = "# Customized PlanMaxx workflow\n"
	if err := os.WriteFile(skillPath, []byte(customSkill), 0o644); err != nil {
		t.Fatalf("modify Claude skill: %v", err)
	}
	legacySkill := filepath.Join(installDir, "skills", "planmaxx", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(legacySkill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacySkill, []byte(planmaxxSkillTestTemplate()), 0o644); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(installDir, ".claude-plugin", "plugin.json"): claudePluginManifest,
		filepath.Join(installDir, "hooks", "hooks.json"):           claudeHooksConfig,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	removeCmd := NewRootCommand(&bytes.Buffer{}, &stderr)
	removeCmd.SetArgs([]string{"skill", "remove", "--target", "claude"})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("Claude skill remove failed: %v", err)
	}

	if got, err := os.ReadFile(skillPath); err != nil || string(got) != customSkill {
		t.Fatalf("modified skill changed: content=%q err=%v", got, err)
	}
	for _, relative := range []string{filepath.Join(".claude-plugin", "plugin.json"), filepath.Join("hooks", "hooks.json")} {
		if _, err := os.Stat(filepath.Join(installDir, relative)); !os.IsNotExist(err) {
			t.Fatalf("managed plugin file %s should be removed, stat err: %v", relative, err)
		}
	}
	if _, err := os.Stat(legacySkill); !os.IsNotExist(err) {
		t.Fatalf("managed nested legacy skill should be removed, stat err: %v", err)
	}
}

func TestSkillInstallClaudeRefusesToOverwriteUnmanagedPluginFile(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	installDir := filepath.Join(home, ".claude", "skills", "planmaxx")
	hookPath := filepath.Join(installDir, "hooks", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatalf("mkdir Claude hooks: %v", err)
	}
	if err := os.WriteFile(hookPath, []byte("{\"custom\":true}\n"), 0o644); err != nil {
		t.Fatalf("write custom Claude hook: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"skill", "install", "--target", "claude", "--copy"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite unmanaged file") {
		t.Fatalf("expected unmanaged plugin file error, got %v", err)
	}
	if _, err := os.Lstat(filepath.Join(installDir, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("preflight should prevent a partial skill install, stat err: %v", err)
	}
}

func TestSkillInstallClaudeRefusesUnmanagedNestedLegacySkillWithoutPartialInstall(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	installDir := filepath.Join(home, ".claude", "skills", "planmaxx")
	legacySkill := filepath.Join(installDir, "skills", "planmaxx", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(legacySkill), 0o755); err != nil {
		t.Fatal(err)
	}
	const custom = "# Custom nested skill\n"
	if err := os.WriteFile(legacySkill, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "claude"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite unmanaged skill") {
		t.Fatalf("expected unmanaged nested skill error, got %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(installDir, "SKILL.md")); !os.IsNotExist(statErr) {
		t.Fatalf("preflight should prevent a partial plain skill install: %v", statErr)
	}
	if got, readErr := os.ReadFile(legacySkill); readErr != nil || string(got) != custom {
		t.Fatalf("custom nested skill changed: content=%q err=%v", got, readErr)
	}
}

func TestSkillInstallClaudeRejectsSymlinkMode(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--target", "claude", "--link"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "use copy mode") {
		t.Fatalf("expected Claude symlink mode rejection, got %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(home, ".claude", "skills", "planmaxx")); !os.IsNotExist(statErr) {
		t.Fatalf("rejected Claude install created files: %v", statErr)
	}
}

func TestSkillInstallRejectsConflictingModes(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	cmd := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "install", "--copy", "--link"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("expected conflicting mode rejection, got %v", err)
	}
}

func TestSkillInstallRefusesToOverwriteUnmanagedSkill(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	installDir := filepath.Join(home, ".claude", "skills", "planmaxx")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(installDir, "SKILL.md")
	const custom = "# My custom PlanMaxx workflow\n"
	if err := os.WriteFile(skillPath, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	cmd := NewRootCommand(&bytes.Buffer{}, &stderr)
	cmd.SetArgs([]string{"skill", "install", "--target", "claude", "--copy"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite unmanaged skill") {
		t.Fatalf("expected unmanaged skill error, got %v", err)
	}
	if got, readErr := os.ReadFile(skillPath); readErr != nil || string(got) != custom {
		t.Fatalf("custom skill changed: content=%q err=%v", got, readErr)
	}
	for _, relative := range []string{filepath.Join(".claude-plugin", "plugin.json"), filepath.Join("hooks", "hooks.json")} {
		if _, statErr := os.Stat(filepath.Join(installDir, relative)); !os.IsNotExist(statErr) {
			t.Fatalf("partial plugin file %s was created: %v", relative, statErr)
		}
	}
}

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

func TestSkillReminderUpdatesSymlinkTargetWithoutReplacingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior is platform-dependent on Windows")
	}

	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "config")
	repoDir := t.TempDir()
	setSkillTestDirs(t, home, configDir)
	SetEmbeddedSkillTemplate([]byte(planmaxxSkillTestTemplate()))

	claudePath := filepath.Join(repoDir, "CLAUDE.md")
	if err := os.WriteFile(claudePath, []byte("# Repository guidance\n"), 0o640); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	agentsPath := filepath.Join(repoDir, "AGENTS.md")
	if err := os.Symlink("CLAUDE.md", agentsPath); err != nil {
		t.Fatalf("symlink AGENTS.md: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	installCmd := NewRootCommand(&stdout, &stderr)
	installCmd.SetArgs([]string{"skill", "install", "--target", "codex", "--repo", repoDir, "--copy"})
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("repo skill install failed: %v", err)
	}

	assertSymlink := func() {
		t.Helper()
		info, err := os.Lstat(agentsPath)
		if err != nil {
			t.Fatalf("lstat AGENTS.md: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("AGENTS.md symlink was replaced")
		}
	}
	assertSymlink()
	claudeBytes, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read CLAUDE.md after install: %v", err)
	}
	if !strings.Contains(string(claudeBytes), planmaxxReminderStart) {
		t.Fatalf("expected reminder in symlink target, got %q", claudeBytes)
	}
	if info, err := os.Stat(claudePath); err != nil {
		t.Fatalf("stat CLAUDE.md: %v", err)
	} else if info.Mode().Perm() != 0o640 {
		t.Fatalf("expected reminder update to preserve mode 0640, got %o", info.Mode().Perm())
	}

	removeCmd := NewRootCommand(&stdout, &stderr)
	removeCmd.SetArgs([]string{"skill", "remove", "--target", "codex", "--repo", repoDir})
	if err := removeCmd.Execute(); err != nil {
		t.Fatalf("repo skill remove failed: %v", err)
	}

	assertSymlink()
	claudeBytes, err = os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read CLAUDE.md after remove: %v", err)
	}
	if strings.Contains(string(claudeBytes), planmaxxReminderStart) {
		t.Fatalf("expected reminder removed from symlink target, got %q", claudeBytes)
	}
	if !strings.Contains(string(claudeBytes), "# Repository guidance") {
		t.Fatalf("expected existing symlink target content to remain, got %q", claudeBytes)
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

func TestSkillHelpShowsTargetAndRepositoryScope(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := NewRootCommand(&stdout, &stderr)
	cmd.SetArgs([]string{"skill", "install", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("skill install help: %v", err)
	}
	for _, visible := range []string{"--repo", "--target", "codex, claude, or grok"} {
		if !strings.Contains(stdout.String(), visible) {
			t.Fatalf("expected skill help to contain %q, got %q", visible, stdout.String())
		}
	}
	for _, hidden := range []string{"--source", "--copy", "--link"} {
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
		"## Supported harnesses",
		"| Manual CLI |",
		"| Codex |",
		"| Claude Code |",
		"| Grok Build |",
		"Core review",
		"`/btw`",
		"Iterate and refine",
		"--install-codex-skill",
		"--install-claude-skill",
		"--install-grok-skill",
		"planmaxx skill install",
		"planmaxx skill remove",
		"~/.agents/skills/planmaxx/",
		"planmaxx skill install --target claude",
		"~/.claude/skills/planmaxx/",
		"planmaxx skill install --target grok",
		".grok/skills/planmaxx/SKILL.md",
		"$GROK_HOME/skills/planmaxx/SKILL.md",
		"skills/planmaxx/SKILL.md",
		"--repo /path/to/repo",
		"--agent none",
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
		"--install-claude-skill",
		"--install-grok-skill",
		"~/.agents/skills",
		"PLANMAXX_INSTALL_CODEX_SKILL",
		"PLANMAXX_INSTALL_CLAUDE_SKILL",
		"PLANMAXX_INSTALL_GROK_SKILL",
		"skill install",
		"skill install --target claude",
		"skill install --target grok",
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
	if !strings.Contains(string(embeddedSkill), "`planmaxx review <plan-file>`") {
		t.Fatal("Codex skill must use the provider-neutral invocation")
	}
	for _, forbidden := range []string{"--claude-session-id", "--grok-session-id", "${SESSION_ID}", "${CLAUDE_SESSION_ID}"} {
		if strings.Contains(string(embeddedSkill), forbidden) {
			t.Fatalf("Codex skill must not contain another provider's invocation marker %q", forbidden)
		}
	}

	claudeSkill, err := os.ReadFile(filepath.Join(root, "internal", "cli", "SKILL.claude.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(claudeSkill), "`planmaxx review --claude-session-id ${CLAUDE_SESSION_ID} <plan-file>`") ||
		!strings.Contains(string(claudeSkill), "invocation-only") {
		t.Fatal("Claude skill must use invocation-scoped Claude session substitution")
	}
	for _, forbidden := range []string{"--grok-session-id", "PLANMAXX_CLAUDE_SESSION_ID", "CLAUDE_ENV_FILE", ".claude-plugin"} {
		if strings.Contains(string(claudeSkill), forbidden) {
			t.Fatalf("Claude skill contains unsafe or cross-provider setup %q", forbidden)
		}
	}

	grokSkill, err := os.ReadFile(filepath.Join(root, "internal", "cli", "SKILL.grok.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(grokSkill), "`planmaxx review --grok-session-id ${SESSION_ID} <plan-file>`") ||
		!strings.Contains(string(grokSkill), "invocation-only") {
		t.Fatal("Grok skill must use invocation-scoped native session substitution")
	}
	for _, forbidden := range []string{"--claude-session-id", "${CLAUDE_SESSION_ID}"} {
		if strings.Contains(string(grokSkill), forbidden) {
			t.Fatalf("Grok skill contains cross-provider invocation %q", forbidden)
		}
	}
}

func setSkillTestDirs(t *testing.T, home string, configDir string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("GROK_HOME", "")
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
