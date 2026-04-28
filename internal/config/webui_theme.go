package config

import "strings"

const (
	WebUIThemeBlueLight    = "blue-light"
	WebUIThemePinkLight    = "pink-light"
	WebUIThemeEmeraldLight = "emerald-light"
	WebUIThemeVioletLight  = "violet-light"
	WebUIThemeAmberLight   = "amber-light"
	WebUIThemeNeutralLight = "neutral-light"
)

var supportedWebUIThemes = []string{
	WebUIThemeBlueLight,
	WebUIThemePinkLight,
	WebUIThemeEmeraldLight,
	WebUIThemeVioletLight,
	WebUIThemeAmberLight,
	WebUIThemeNeutralLight,
}

func NormalizeWebUITheme(theme string) string {
	switch strings.TrimSpace(strings.ToLower(theme)) {
	case "blue", "blue-light", "blue_light":
		return WebUIThemeBlueLight
	case "", "pink", "pink-light", "pink_light":
		return WebUIThemePinkLight
	case "emerald", "emerald-light", "emerald_light", "green", "green-light", "green_light":
		return WebUIThemeEmeraldLight
	case "violet", "violet-light", "violet_light", "purple", "purple-light", "purple_light":
		return WebUIThemeVioletLight
	case "amber", "amber-light", "amber_light", "orange", "orange-light", "orange_light":
		return WebUIThemeAmberLight
	case "neutral", "neutral-light", "neutral_light", "gray", "grey", "gray-light", "grey-light":
		return WebUIThemeNeutralLight
	default:
		return strings.TrimSpace(strings.ToLower(theme))
	}
}

func SupportedWebUIThemes() []string {
	return append([]string(nil), supportedWebUIThemes...)
}

func SupportedWebUIThemeList(separator string) string {
	return strings.Join(supportedWebUIThemes, separator)
}

func IsSupportedWebUITheme(theme string) bool {
	switch NormalizeWebUITheme(theme) {
	case WebUIThemeBlueLight, WebUIThemePinkLight, WebUIThemeEmeraldLight, WebUIThemeVioletLight, WebUIThemeAmberLight, WebUIThemeNeutralLight:
		return true
	default:
		return false
	}
}
