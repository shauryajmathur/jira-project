package jiramcp

import (
	"encoding/json"
	"fmt"
	"os"

	"jira-project/internal/config"
)

// WriteConfig saves the Jira connection settings for one local MCP process.
// Callers should place this file outside the agent workspace and remove it when
// generation finishes.
func WriteConfig(path string, cfg config.Config) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encodeErr := encoder.Encode(cfg)
	closeErr := file.Close()
	if encodeErr != nil {
		_ = os.Remove(path)
		return encodeErr
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

// ReadConfig reads a private configuration passed by the parent dashboard.
func ReadConfig(path string) (config.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return config.Config{}, fmt.Errorf("open MCP configuration: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return config.Config{}, fmt.Errorf("inspect MCP configuration: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return config.Config{}, fmt.Errorf("MCP configuration must have owner-only permissions")
	}
	var cfg config.Config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return config.Config{}, fmt.Errorf("read MCP configuration: %w", err)
	}
	return cfg, nil
}
