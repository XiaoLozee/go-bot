package ai

import "time"

type Snapshot struct {
	Enabled                bool             `json:"enabled"`
	Ready                  bool             `json:"ready"`
	State                  string           `json:"state"`
	ProviderKind           string           `json:"provider_kind,omitempty"`
	ProviderVendor         string           `json:"provider_vendor,omitempty"`
	Model                  string           `json:"model,omitempty"`
	VisionEnabled          bool             `json:"vision_enabled"`
	VisionMode             string           `json:"vision_mode,omitempty"`
	VisionProvider         string           `json:"vision_provider,omitempty"`
	VisionModel            string           `json:"vision_model,omitempty"`
	StoreEngine            string           `json:"store_engine,omitempty"`
	StoreReady             bool             `json:"store_ready"`
	SessionCount           int              `json:"session_count"`
	CandidateCount         int              `json:"candidate_count"`
	LongTermCount          int              `json:"long_term_count"`
	GroupProfileCount      int              `json:"group_profile_count"`
	UserProfileCount       int              `json:"user_profile_count"`
	RelationEdgeCount      int              `json:"relation_edge_count"`
	SkillProviderCount     int              `json:"skill_provider_count"`
	SkillToolCount         int              `json:"skill_tool_count"`
	PrivatePersonaCount    int              `json:"private_persona_count"`
	PrivateActivePersona   string           `json:"private_active_persona,omitempty"`
	PrivateActivePersonaID string           `json:"private_active_persona_id,omitempty"`
	ReflectionRunning      bool             `json:"reflection_running"`
	LastReplyAt            time.Time        `json:"last_reply_at,omitempty"`
	LastVisionAt           time.Time        `json:"last_vision_at,omitempty"`
	LastReflectionAt       time.Time        `json:"last_reflection_at,omitempty"`
	LastVisionSummary      string           `json:"last_vision_summary,omitempty"`
	LastReflectionSummary  string           `json:"last_reflection_summary,omitempty"`
	LastReflectionStats    *ReflectionStats `json:"last_reflection_stats,omitempty"`
	LastVisionError        string           `json:"last_vision_error,omitempty"`
	LastReflectionError    string           `json:"last_reflection_error,omitempty"`
	LastError              string           `json:"last_error,omitempty"`
	LastDecisionReason     string           `json:"last_decision_reason,omitempty"`
}

type SkillView struct {
	ProviderID  string          `json:"provider_id"`
	Source      string          `json:"source"`
	PluginID    string          `json:"plugin_id,omitempty"`
	Namespace   string          `json:"namespace"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	ToolCount   int             `json:"tool_count"`
	Tools       []SkillToolView `json:"tools,omitempty"`
}

type SkillToolView struct {
	Name               string `json:"name"`
	Description        string `json:"description,omitempty"`
	DisplayName        string `json:"display_name,omitempty"`
	DisplayDescription string `json:"display_description,omitempty"`
	Availability       string `json:"availability,omitempty"`
}

type PromptSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content"`
}

type DebugView struct {
	Sessions          []SessionDebugView   `json:"sessions,omitempty"`
	CandidateMemories []CandidateMemory    `json:"candidate_memories,omitempty"`
	LongTermMemories  []LongTermMemory     `json:"long_term_memories,omitempty"`
	GroupProfiles     []GroupProfile       `json:"group_profiles,omitempty"`
	GroupObservations []GroupObservation   `json:"group_observations,omitempty"`
	UserProfiles      []UserInGroupProfile `json:"user_profiles,omitempty"`
	RelationEdges     []RelationEdge       `json:"relation_edges,omitempty"`
	ReflectionStats   *ReflectionStats     `json:"reflection_stats,omitempty"`
	MCPServers        []MCPServerDebugView `json:"mcp_servers,omitempty"`
}

type SessionDebugView struct {
	Scope         string                `json:"scope"`
	GroupID       string                `json:"group_id,omitempty"`
	TopicSummary  string                `json:"topic_summary,omitempty"`
	ActiveUsers   []string              `json:"active_users,omitempty"`
	RecentCount   int                   `json:"recent_count"`
	RecentPreview []ConversationMessage `json:"recent_preview,omitempty"`
	LastBotAction *BotAction            `json:"last_bot_action,omitempty"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

type ReplyGateResult struct {
	ShouldReply bool   `json:"should_reply"`
	Mode        string `json:"mode"`
	LearnOnly   bool   `json:"learn_only"`
	Priority    string `json:"priority"`
	Reason      string `json:"reason"`
}

type ReplyPlan struct {
	ShouldReply  bool     `json:"should_reply"`
	ReplyMode    string   `json:"reply_mode"`
	Tone         string   `json:"tone"`
	Length       string   `json:"length"`
	TargetUser   string   `json:"target_user,omitempty"`
	UseMemory    bool     `json:"use_memory"`
	MemoryRefs   []string `json:"memory_refs,omitempty"`
	ResponseGoal string   `json:"response_goal"`
	RiskLevel    string   `json:"risk_level"`
}

type SessionState struct {
	Scope         string                `json:"scope"`
	GroupID       string                `json:"group_id,omitempty"`
	Recent        []ConversationMessage `json:"recent_messages,omitempty"`
	TopicSummary  string                `json:"topic_summary,omitempty"`
	ActiveUsers   []string              `json:"active_users,omitempty"`
	LastBotAction *BotAction            `json:"last_bot_action,omitempty"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

type ConversationMessage struct {
	Role             string    `json:"role"`
	UserID           string    `json:"user_id,omitempty"`
	UserName         string    `json:"user_name,omitempty"`
	Text             string    `json:"text"`
	ReasoningContent string    `json:"reasoning_content,omitempty"`
	MessageID        string    `json:"message_id,omitempty"`
	At               time.Time `json:"at"`
}

type BotAction struct {
	Mode      string    `json:"mode"`
	Accepted  bool      `json:"accepted"`
	MessageID string    `json:"message_id,omitempty"`
	At        time.Time `json:"at"`
}

type CandidateMemory struct {
	ID            string    `json:"id"`
	Scope         string    `json:"scope"`
	MemoryType    string    `json:"memory_type"`
	Subtype       string    `json:"subtype"`
	SubjectID     string    `json:"subject_id,omitempty"`
	GroupID       string    `json:"group_id,omitempty"`
	Content       string    `json:"content"`
	Confidence    float64   `json:"confidence"`
	EvidenceCount int       `json:"evidence_count"`
	SourceMsgIDs  []string  `json:"source_msg_ids,omitempty"`
	Status        string    `json:"status"`
	TTLDays       int       `json:"ttl_days"`
	CreatedAt     time.Time `json:"created_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
}

type LongTermMemory struct {
	ID            string    `json:"id"`
	Scope         string    `json:"scope"`
	MemoryType    string    `json:"memory_type"`
	Subtype       string    `json:"subtype"`
	SubjectID     string    `json:"subject_id,omitempty"`
	GroupID       string    `json:"group_id,omitempty"`
	Content       string    `json:"content"`
	Confidence    float64   `json:"confidence"`
	EvidenceCount int       `json:"evidence_count"`
	SourceRefs    []string  `json:"source_refs,omitempty"`
	TTLDays       int       `json:"ttl_days"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type GroupProfile struct {
	GroupID           string    `json:"group_id"`
	StyleTags         []string  `json:"style_tags,omitempty"`
	TopicFocus        []string  `json:"topic_focus,omitempty"`
	ActiveMemes       []string  `json:"active_memes,omitempty"`
	SoftRules         []string  `json:"soft_rules,omitempty"`
	HardRules         []string  `json:"hard_rules,omitempty"`
	ReflectionSummary string    `json:"reflection_summary,omitempty"`
	HumorDensity      float64   `json:"humor_density"`
	EmojiRate         float64   `json:"emoji_rate"`
	Formality         float64   `json:"formality"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type ReflectionStats struct {
	AdjustedCandidateCount int `json:"adjusted_candidate_count,omitempty"`
	ConflictCandidateCount int `json:"conflict_candidate_count,omitempty"`
	StableCandidateCount   int `json:"stable_candidate_count,omitempty"`
	CoolingCandidateCount  int `json:"cooling_candidate_count,omitempty"`
	PromotedCount          int `json:"promoted_count,omitempty"`
	DeletedCandidateCount  int `json:"deleted_candidate_count,omitempty"`
	DeletedLongTermCount   int `json:"deleted_long_term_count,omitempty"`
	UpdatedGroupCount      int `json:"updated_group_count,omitempty"`
	ReflectedMemeCount     int `json:"reflected_meme_count,omitempty"`
}

type GroupObservation struct {
	GroupID             string            `json:"group_id"`
	Summary             string            `json:"summary,omitempty"`
	TopicSummary        string            `json:"topic_summary,omitempty"`
	StyleTags           []string          `json:"style_tags,omitempty"`
	TopicFocus          []string          `json:"topic_focus,omitempty"`
	ActiveMemes         []string          `json:"active_memes,omitempty"`
	ActiveUsers         []string          `json:"active_users,omitempty"`
	RiskFlags           []string          `json:"risk_flags,omitempty"`
	SessionMessageCount int               `json:"session_message_count,omitempty"`
	CandidateHighlights []CandidateMemory `json:"candidate_highlights,omitempty"`
	LongTermHighlights  []LongTermMemory  `json:"long_term_highlights,omitempty"`
	RelationHighlights  []RelationEdge    `json:"relation_highlights,omitempty"`
	UpdatedAt           time.Time         `json:"updated_at,omitempty"`
}

type UserInGroupProfile struct {
	GroupID                 string    `json:"group_id"`
	UserID                  string    `json:"user_id"`
	DisplayName             string    `json:"display_name,omitempty"`
	Nicknames               []string  `json:"nicknames,omitempty"`
	TopicPreferences        []string  `json:"topic_preferences,omitempty"`
	StyleTags               []string  `json:"style_tags,omitempty"`
	TabooTopics             []string  `json:"taboo_topics,omitempty"`
	InteractionLevelWithBot int       `json:"interaction_level_with_bot"`
	TeasingTolerance        float64   `json:"teasing_tolerance"`
	TrustScore              float64   `json:"trust_score"`
	LastActiveAt            time.Time `json:"last_active_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type RelationEdge struct {
	ID                string    `json:"id"`
	GroupID           string    `json:"group_id"`
	NodeA             string    `json:"node_a"`
	NodeB             string    `json:"node_b"`
	RelationType      string    `json:"relation_type"`
	Strength          float64   `json:"strength"`
	EvidenceCount     int       `json:"evidence_count"`
	LastInteractionAt time.Time `json:"last_interaction_at"`
}

type chatMessage struct {
	Role             string     `json:"role"`
	Content          any        `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []toolCall `json:"tool_calls,omitempty"`
}

type MessageLogQuery struct {
	ChatType string `json:"chat_type,omitempty"`
	GroupID  string `json:"group_id,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	Keyword  string `json:"keyword,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type MessageSuggestionQuery struct {
	ChatType string `json:"chat_type,omitempty"`
	GroupID  string `json:"group_id,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type MessageSearchSuggestions struct {
	Groups []string `json:"groups,omitempty"`
	Users  []string `json:"users,omitempty"`
}

type MessageLog struct {
	MessageID        string    `json:"message_id"`
	ConnectionID     string    `json:"connection_id"`
	ChatType         string    `json:"chat_type"`
	GroupID          string    `json:"group_id,omitempty"`
	GroupName        string    `json:"group_name,omitempty"`
	UserID           string    `json:"user_id,omitempty"`
	SenderRole       string    `json:"sender_role"`
	SenderName       string    `json:"sender_name,omitempty"`
	SenderNickname   string    `json:"sender_nickname,omitempty"`
	ReplyToMessageID string    `json:"reply_to_message_id,omitempty"`
	TextContent      string    `json:"text_content,omitempty"`
	NormalizedHash   string    `json:"normalized_hash,omitempty"`
	HasText          bool      `json:"has_text"`
	HasImage         bool      `json:"has_image"`
	MessageStatus    string    `json:"message_status,omitempty"`
	OccurredAt       time.Time `json:"occurred_at"`
	CreatedAt        time.Time `json:"created_at"`
	ImageCount       int       `json:"image_count,omitempty"`
}

type MessageImage struct {
	ID             string    `json:"id"`
	MessageID      string    `json:"message_id"`
	SegmentIndex   int       `json:"segment_index"`
	OriginRef      string    `json:"origin_ref,omitempty"`
	VisionSummary  string    `json:"vision_summary,omitempty"`
	VisionStatus   string    `json:"vision_status,omitempty"`
	AssetStatus    string    `json:"asset_status,omitempty"`
	AssetError     string    `json:"asset_error,omitempty"`
	StorageBackend string    `json:"storage_backend,omitempty"`
	StorageKey     string    `json:"storage_key,omitempty"`
	PublicURL      string    `json:"public_url,omitempty"`
	FileName       string    `json:"file_name,omitempty"`
	MimeType       string    `json:"mime_type,omitempty"`
	SizeBytes      int64     `json:"size_bytes,omitempty"`
	SHA256         string    `json:"sha256,omitempty"`
	PreviewURL     string    `json:"preview_url,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type MessageDetail struct {
	Message MessageLog     `json:"message"`
	Images  []MessageImage `json:"images,omitempty"`
}

type MessageImagePreview struct {
	MessageID    string `json:"message_id"`
	SegmentIndex int    `json:"segment_index"`
	MimeType     string `json:"mime_type,omitempty"`
	LocalPath    string `json:"local_path,omitempty"`
	RedirectURL  string `json:"redirect_url,omitempty"`
}
