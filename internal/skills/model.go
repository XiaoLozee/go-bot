package skills

import "time"

type InstalledSkill struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Description        string    `json:"description,omitempty"`
	SourceType         string    `json:"source_type"`
	SourceLabel        string    `json:"source_label,omitempty"`
	SourceURL          string    `json:"source_url,omitempty"`
	Provider           string    `json:"provider,omitempty"`
	Enabled            bool      `json:"enabled"`
	InstalledAt        time.Time `json:"installed_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	EntryPath          string    `json:"entry_path"`
	RootDir            string    `json:"-"`
	Format             string    `json:"format,omitempty"`
	InstructionPreview string    `json:"instruction_preview,omitempty"`
	ContentLength      int       `json:"content_length,omitempty"`
}

type SkillDetail struct {
	InstalledSkill
	Content string `json:"content,omitempty"`
}

type PromptSkill struct {
	ID          string
	Name        string
	Description string
	Content     string
}

type InstallResult struct {
	Skill       SkillDetail
	InstalledTo string
	BackupPath  string
	Replaced    bool
}
