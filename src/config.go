package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// Config 是 /data/adb/band-ai/config.json 的结构。
type Config struct {
	Listen     string `json:"listen"`      // 系统代理模式 127.0.0.1:8080
	TProxyListen string `json:"tproxy_listen"` // 透明代理模式 127.0.0.1:8081
	APIMode    string `json:"api_mode"`    // openai | custom
	APIURL     string `json:"api_url"`     // openai: 完整端点; custom: 桥地址
	APIKey     string `json:"api_key"`     // openai 模式必填
	Model      string `json:"model"`       // openai 模式必填
	SystemPrompt string `json:"system_prompt"`
	TargetHost string `json:"target_host"` // speech.ai.xiaomi.com
	MaxReply   int    `json:"max_reply_chars"`
	LogDir     string `json:"log_dir"`

	DeviceNamespaces []string `json:"device_namespaces"`
	DeviceTemplates  []string `json:"device_templates"`
	DeviceKeywords   []string `json:"device_keywords"`
}

const defaultSystemPrompt = "你正在回答一个智能手环用户，答案显示在不到 3 厘米宽的小屏幕上。" +
	"规则：只输出一句话；不超过 40 个汉字；不要换行；不要 Markdown；不要列举；" +
	"不要解释你在做什么；直接给结论。问天气就给天气和温度，问知识就给核心结论，问笑话就讲一个短的。"

func DefaultConfig() *Config {
	return &Config{
		Listen:       "127.0.0.1:8080",
		TProxyListen: "127.0.0.1:8081",
		APIMode:      "openai",
		APIURL:       "https://api.deepseek.com/chat/completions",
		Model:        "deepseek-chat",
		SystemPrompt: defaultSystemPrompt,
		TargetHost:   "speech.ai.xiaomi.com",
		MaxReply:     60,
		LogDir:       "/data/adb/band-ai/logs",
		DeviceNamespaces: []string{
			"Alerts", "Alarm", "Timer", "Reminder", "Device", "MiHome", "Scene",
		},
		DeviceTemplates: []string{
			"Alarm", "Timer", "Reminder", "SwitchPanel", "Device", "DeviceList",
			"Scene", "Weather", "Calendar", "Call", "Message",
		},
		DeviceKeywords: []string{
			"闹钟", "闹铃", "计时", "倒计时", "定时", "提醒", "叫我", "叫醒",
			"关闭", "打开", "调高", "调低", "音量", "亮度",
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil // 用默认值
		}
		return nil, err
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, err
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8080"
	}
	if cfg.TProxyListen == "" {
		cfg.TProxyListen = "127.0.0.1:8081"
	}
	if cfg.APIMode == "" {
		cfg.APIMode = "openai"
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = defaultSystemPrompt
	}
	if cfg.TargetHost == "" {
		cfg.TargetHost = "speech.ai.xiaomi.com"
	}
	if cfg.MaxReply <= 0 {
		cfg.MaxReply = 60
	}
	return cfg, nil
}

func (c *Config) inSet(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// IsDeviceNamespace 判断某个 namespace 是否属于设备控制。
func (c *Config) IsDeviceNamespace(ns string) bool {
	return c.inSet(c.DeviceNamespaces, ns)
}

// IsDeviceTemplate 判断某个 Template.name 是否属于设备卡片。
func (c *Config) IsDeviceTemplate(name string) bool {
	return c.inSet(c.DeviceTemplates, name)
}

// HasDeviceKeyword 关键词兜底：指令帧和文案帧可能只差 1ms，顺序会翻转。
func (c *Config) HasDeviceKeyword(q string) bool {
	for _, k := range c.DeviceKeywords {
		if strings.Contains(q, k) {
			return true
		}
	}
	return false
}
