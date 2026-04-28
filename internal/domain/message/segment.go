package message

type Segment struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

func Text(text string) Segment {
	return Segment{
		Type: "text",
		Data: map[string]any{"text": text},
	}
}

func At(qq string) Segment {
	return Segment{
		Type: "at",
		Data: map[string]any{"qq": qq},
	}
}

func Reply(messageID string) Segment {
	return Segment{
		Type: "reply",
		Data: map[string]any{"id": messageID},
	}
}

func Image(file string) Segment {
	return Segment{
		Type: "image",
		Data: map[string]any{"file": file},
	}
}

func Video(file string) Segment {
	return Segment{
		Type: "video",
		Data: map[string]any{"file": file},
	}
}

func File(file, name string) Segment {
	data := map[string]any{"file": file}
	if name != "" {
		data["name"] = name
	}
	return Segment{
		Type: "file",
		Data: data,
	}
}

func MusicCustom(url, audio, title, content, image string) Segment {
	data := map[string]any{
		"type": "custom",
	}
	if url != "" {
		data["url"] = url
	}
	if audio != "" {
		data["audio"] = audio
	}
	if title != "" {
		data["title"] = title
	}
	if content != "" {
		data["content"] = content
	}
	if image != "" {
		data["image"] = image
	}
	return Segment{
		Type: "music",
		Data: data,
	}
}
