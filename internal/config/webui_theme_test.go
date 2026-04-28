package config

import "testing"

func TestNormalizeWebUITheme_SupportsExtendedAliases(t *testing.T) {
	tests := map[string]string{
		"blue":         WebUIThemeBlueLight,
		"pink":         WebUIThemePinkLight,
		"green":        WebUIThemeEmeraldLight,
		"emerald":      WebUIThemeEmeraldLight,
		"purple":       WebUIThemeVioletLight,
		"violet-light": WebUIThemeVioletLight,
		"orange":       WebUIThemeAmberLight,
		"amber-light":  WebUIThemeAmberLight,
		"gray":         WebUIThemeNeutralLight,
		"grey-light":   WebUIThemeNeutralLight,
	}

	for input, want := range tests {
		if got := NormalizeWebUITheme(input); got != want {
			t.Fatalf("NormalizeWebUITheme(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIsSupportedWebUITheme_AllowsAllPresetThemes(t *testing.T) {
	for _, theme := range SupportedWebUIThemes() {
		if !IsSupportedWebUITheme(theme) {
			t.Fatalf("IsSupportedWebUITheme(%q) = false, want true", theme)
		}
	}
}
