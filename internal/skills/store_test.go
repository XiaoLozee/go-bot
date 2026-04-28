package skills

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreInstallArchive_EnableDisableUninstall(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	payload := buildSkillZip(t, map[string]string{
		"demo-skill/SKILL.md":              "# Demo Skill\n\nThis skill helps with code review and concise answers.\n",
		"demo-skill/references/example.md": "example",
	})

	result, err := store.InstallArchive(context.Background(), "demo-skill.zip", payload, false)
	if err != nil {
		t.Fatalf("InstallArchive() error = %v", err)
	}
	if result.Skill.ID == "" {
		t.Fatalf("skill id = empty, want generated id")
	}
	if result.Skill.Name != "Demo Skill" {
		t.Fatalf("skill name = %q, want Demo Skill", result.Skill.Name)
	}
	if !result.Skill.Enabled {
		t.Fatalf("skill enabled = false, want true after install")
	}
	if _, err := os.Stat(filepath.Join(result.InstalledTo, "SKILL.md")); err != nil {
		t.Fatalf("Stat(SKILL.md) error = %v", err)
	}

	items := store.List()
	if len(items) != 1 {
		t.Fatalf("List() len = %d, want 1", len(items))
	}
	if items[0].InstructionPreview == "" {
		t.Fatalf("InstructionPreview = empty, want preview")
	}

	prompts, err := store.EnabledPromptSkills()
	if err != nil {
		t.Fatalf("EnabledPromptSkills() error = %v", err)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0].Content, "code review") {
		t.Fatalf("prompt skills = %+v, want installed skill content", prompts)
	}

	updated, err := store.SetEnabled(result.Skill.ID, false)
	if err != nil {
		t.Fatalf("SetEnabled(false) error = %v", err)
	}
	if updated.Enabled {
		t.Fatalf("updated enabled = true, want false")
	}
	prompts, err = store.EnabledPromptSkills()
	if err != nil {
		t.Fatalf("EnabledPromptSkills(after disable) error = %v", err)
	}
	if len(prompts) != 0 {
		t.Fatalf("prompt skills len = %d, want 0 after disable", len(prompts))
	}

	removed, err := store.Uninstall(result.Skill.ID)
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if removed.ID != result.Skill.ID {
		t.Fatalf("removed id = %q, want %q", removed.ID, result.Skill.ID)
	}
	if _, err := os.Stat(result.InstalledTo); !os.IsNotExist(err) {
		t.Fatalf("Stat(installed dir) error = %v, want not exists", err)
	}
	if len(store.List()) != 0 {
		t.Fatalf("List() len = %d, want 0 after uninstall", len(store.List()))
	}
}

func TestStoreInstallArchive_OverwriteCreatesBackup(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	firstPayload := buildSkillZip(t, map[string]string{
		"demo-skill/SKILL.md": "# Demo Skill\n\nFirst version.\n",
	})
	first, err := store.InstallArchive(context.Background(), "demo-skill.zip", firstPayload, false)
	if err != nil {
		t.Fatalf("InstallArchive(first) error = %v", err)
	}
	secondPayload := buildSkillTarGZ(t, map[string]string{
		"demo-skill/SKILL.md": "# Demo Skill\n\nSecond version with richer content.\n",
	})
	if _, err := store.InstallArchive(context.Background(), "demo-skill.tar.gz", secondPayload, false); err == nil || !strings.Contains(err.Error(), "覆盖安装") {
		t.Fatalf("InstallArchive(no overwrite) error = %v, want overwrite hint", err)
	}
	second, err := store.InstallArchive(context.Background(), "demo-skill.tar.gz", secondPayload, true)
	if err != nil {
		t.Fatalf("InstallArchive(overwrite) error = %v", err)
	}
	if !second.Replaced {
		t.Fatalf("Replaced = false, want true")
	}
	if second.BackupPath == "" {
		t.Fatalf("BackupPath = empty, want backup path")
	}
	if _, err := os.Stat(second.BackupPath); err != nil {
		t.Fatalf("Stat(backup path) error = %v", err)
	}
	backupContent, err := os.ReadFile(filepath.Join(second.BackupPath, "SKILL.md"))
	if err != nil {
		t.Fatalf("ReadFile(backup skill) error = %v", err)
	}
	if !strings.Contains(string(backupContent), "First version") {
		t.Fatalf("backup content = %q, want first version", string(backupContent))
	}
	current, err := store.Get(first.Skill.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !strings.Contains(current.Content, "Second version") {
		t.Fatalf("current content = %q, want second version", current.Content)
	}
}

func TestDirectArchiveCandidate(t *testing.T) {
	candidate := directArchiveCandidate("https://example.com/archive/demo-skill.tar.gz")
	if candidate == nil {
		t.Fatalf("directArchiveCandidate() = nil, want archive candidate")
	}
	if candidate.FileName != "demo-skill.tar.gz" {
		t.Fatalf("candidate file name = %q, want demo-skill.tar.gz", candidate.FileName)
	}
}

func TestRepositoryArchiveCandidates_UnsupportedHost(t *testing.T) {
	parsed, err := normalizeRemoteURL("https://skillhub.cloud.tencent.com/skills/demo/demo-skill")
	if err != nil {
		t.Fatalf("normalizeRemoteURL() error = %v", err)
	}
	candidates := repositoryArchiveCandidates(parsed)
	if len(candidates) != 0 {
		t.Fatalf("repositoryArchiveCandidates() = %+v, want unsupported host to be rejected", candidates)
	}
}

func TestRepositoryArchiveCandidates_GitHub(t *testing.T) {
	parsed, err := normalizeRemoteURL("https://github.com/demo/demo-skill")
	if err != nil {
		t.Fatalf("normalizeRemoteURL() error = %v", err)
	}
	candidates := repositoryArchiveCandidates(parsed)
	if len(candidates) != 2 {
		t.Fatalf("repositoryArchiveCandidates() len = %d, want 2", len(candidates))
	}
	if !strings.Contains(candidates[0].URL, "codeload.github.com/demo/demo-skill") {
		t.Fatalf("candidate url = %q, want GitHub codeload url", candidates[0].URL)
	}
}

func buildSkillZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("writer.Create(%s) error = %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("entry.Write(%s) error = %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}
	return buf.Bytes()
}

func buildSkillTarGZ(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range files {
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("tarWriter.WriteHeader(%s) error = %v", name, err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("tarWriter.Write(%s) error = %v", name, err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tarWriter.Close() error = %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzipWriter.Close() error = %v", err)
	}
	return buf.Bytes()
}
