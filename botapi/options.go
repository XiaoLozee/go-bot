package botapi

type ImageOption func(*FileData)

// WithSummary 设置图片外显
func WithSummary(summary string) ImageOption {
	return func(data *FileData) {
		data.Summary = summary
	}
}
