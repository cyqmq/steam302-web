package prefer

import (
	"encoding/json"
	"errors"
	"os"
)

const SchemaVersion = 1

// Path 默认缓存位置（相对项目根）。
const Path = "config/prefer.json"

// Load 读取缓存；文件不存在返回 nil 缓存（非错误）。
func Load(path string) (*Cache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save 写缓存；旧文件不存在时忽略 Remove 错误。
func Save(path string, c *Cache) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Remove 删除缓存文件。
func Remove(path string) error {
	return os.Remove(path)
}

// Entry 按 ruleID+siteIndex 查缓存项。
func (c *Cache) Entry(ruleID string, siteIndex int) *Entry {
	if c == nil {
		return nil
	}
	for i := range c.Entries {
		if c.Entries[i].RuleID == ruleID && c.Entries[i].SiteIndex == siteIndex {
			return &c.Entries[i]
		}
	}
	return nil
}
