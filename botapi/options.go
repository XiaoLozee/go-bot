package botapi

type ImageOption func(*FileData)

// WithSummary 设置图片外显
func WithSummary(summary string) ImageOption {
	return func(data *FileData) {
		data.Summary = summary
	}
}

type ForwardOption func(params *GroupForwardMsgParams)

// WithPrompt 设置合并转发消息的外显摘要。
func WithPrompt(prompt string) ForwardOption {
	return func(p *GroupForwardMsgParams) {
		p.Prompt = prompt
	}
}

// WithForwardSummary 设置合并转发消息的底部摘要。
func WithForwardSummary(summary string) ForwardOption {
	return func(p *GroupForwardMsgParams) {
		p.Summary = summary
	}
}

// WithSource 设置合并转发消息的顶部来源。
func WithSource(source string) ForwardOption {
	return func(p *GroupForwardMsgParams) {
		p.Source = source
	}
}
