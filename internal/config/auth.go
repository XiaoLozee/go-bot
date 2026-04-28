package config

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

const passwordHashPrefix = "sha256"

func IsAdminPasswordHashed(value string) bool {
	parts := strings.Split(value, "$")
	return len(parts) == 3 && parts[0] == passwordHashPrefix
}

func ValidateAdminPassword(password string) error {
	trimmed := strings.TrimSpace(password)
	if len(trimmed) < 6 {
		return fmt.Errorf("后台密码至少需要 6 个字符")
	}
	if len(trimmed) > 128 {
		return fmt.Errorf("后台密码长度不能超过 128 个字符")
	}
	return nil
}

func HashAdminPassword(password string) (string, error) {
	if err := ValidateAdminPassword(password); err != nil {
		return "", err
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成密码盐失败: %w", err)
	}

	sum := sha256.Sum256(append(salt, []byte(password)...))
	return passwordHashPrefix + "$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(sum[:]), nil
}

func VerifyAdminPassword(stored, input string) bool {
	if stored == "" {
		return false
	}

	parts := strings.Split(stored, "$")
	if !IsAdminPasswordHashed(stored) {
		return subtle.ConstantTimeCompare([]byte(stored), []byte(input)) == 1
	}

	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}

	sum := sha256.Sum256(append(salt, []byte(input)...))
	return subtle.ConstantTimeCompare(expected, sum[:]) == 1
}
