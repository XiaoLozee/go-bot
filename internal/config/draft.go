package config

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/spf13/viper"
)

func DecodeDraftMap(raw map[string]any) (*Config, error) {
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("编码配置草稿失败: %w", err)
	}

	v := viper.New()
	v.SetConfigType("json")
	setDefaults(v)

	if err := v.ReadConfig(bytes.NewReader(payload)); err != nil {
		return nil, fmt.Errorf("读取配置草稿失败: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("解析配置草稿失败: %w", err)
	}
	NormalizeConfig(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
