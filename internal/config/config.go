package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

var configDir = filepath.Join(homeDir(), ".puff")
var configFile = filepath.Join(configDir, "config.toml")

// Config lived in ~/.tpuff before the rename to puff. The directory holds API
// keys, so the migration copies rather than moves: a user who downgrades, or
// who runs both binaries during the transition, still has a working ~/.tpuff.
var legacyConfigDir = filepath.Join(homeDir(), ".tpuff")
var legacyConfigFile = filepath.Join(legacyConfigDir, "config.toml")

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// migrateLegacyConfig copies ~/.tpuff/config.toml to ~/.puff/config.toml when
// the new path is absent and the old one exists. Best effort: a failure here
// leaves the caller looking at a fresh config, which is what it would have
// seen anyway.
func migrateLegacyConfig() {
	if _, err := os.Stat(configFile); err == nil {
		return
	}
	data, err := os.ReadFile(legacyConfigFile)
	if err != nil {
		return
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return
	}
	if err := os.WriteFile(configFile, data, 0600); err != nil {
		return
	}
	fmt.Fprintf(os.Stderr, "puff: migrated config from %s to %s\n", legacyConfigDir, configDir)
}

type Config struct {
	Active string               `toml:"active"`
	Envs   map[string]EnvConfig `toml:"envs"`
}

type EnvConfig struct {
	APIKey        string            `toml:"api_key"`
	Region        string            `toml:"region"`
	BaseURL       string            `toml:"base_url,omitempty"`
	ContentField  string            `toml:"content_field,omitempty"`
	ContentFields map[string]string `toml:"content_fields,omitempty"`
}

// GetContentField returns the content field for a given namespace.
// Priority: namespace-specific override > env default > "".
func (e EnvConfig) GetContentField(namespace string) string {
	if e.ContentFields != nil {
		if f, ok := e.ContentFields[namespace]; ok {
			return f
		}
	}
	return e.ContentField
}

// Load reads config from ~/.puff/config.toml. Returns empty config if not found.
func Load() Config {
	migrateLegacyConfig()
	cfg := Config{Envs: make(map[string]EnvConfig)}
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		return cfg
	}
	if _, err := toml.DecodeFile(configFile, &cfg); err != nil {
		return Config{Envs: make(map[string]EnvConfig)}
	}
	if cfg.Envs == nil {
		cfg.Envs = make(map[string]EnvConfig)
	}
	return cfg
}

// Save writes config to ~/.puff/config.toml with secure permissions.
func Save(cfg Config) error {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(configFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return toml.NewEncoder(f).Encode(cfg)
}

// GetActiveEnv returns the active environment name and config, or empty if none.
func GetActiveEnv() (string, EnvConfig, bool) {
	cfg := Load()
	if cfg.Active == "" {
		return "", EnvConfig{}, false
	}
	env, ok := cfg.Envs[cfg.Active]
	if !ok {
		return "", EnvConfig{}, false
	}
	return cfg.Active, env, true
}

// GetEnv returns a specific named environment.
func GetEnv(name string) (EnvConfig, bool) {
	cfg := Load()
	env, ok := cfg.Envs[name]
	return env, ok
}

// ListEnvs returns all environments with their active status.
func ListEnvs() []EnvEntry {
	cfg := Load()
	var entries []EnvEntry
	for name, env := range cfg.Envs {
		entries = append(entries, EnvEntry{
			Name:     name,
			Config:   env,
			IsActive: name == cfg.Active,
		})
	}
	return entries
}

type EnvEntry struct {
	Name     string
	Config   EnvConfig
	IsActive bool
}

// AddEnv adds or overwrites an environment.
func AddEnv(name, apiKey, region, baseURL string) error {
	cfg := Load()
	cfg.Envs[name] = EnvConfig{
		APIKey:  apiKey,
		Region:  region,
		BaseURL: baseURL,
	}
	// Set as active if first env
	if cfg.Active == "" {
		cfg.Active = name
	}
	return Save(cfg)
}

// RemoveEnv removes an environment.
func RemoveEnv(name string) error {
	cfg := Load()
	if _, ok := cfg.Envs[name]; !ok {
		return fmt.Errorf("environment '%s' not found", name)
	}
	delete(cfg.Envs, name)
	if cfg.Active == name {
		cfg.Active = ""
		for k := range cfg.Envs {
			cfg.Active = k
			break
		}
	}
	return Save(cfg)
}

// SetActive sets the active environment.
func SetActive(name string) error {
	cfg := Load()
	if _, ok := cfg.Envs[name]; !ok {
		return fmt.Errorf("environment '%s' not found", name)
	}
	cfg.Active = name
	return Save(cfg)
}

// GetActiveContentField returns the content field for the active env and namespace.
func GetActiveContentField(namespace string) string {
	_, env, ok := GetActiveEnv()
	if !ok {
		return ""
	}
	return env.GetContentField(namespace)
}

// SetContentField sets the content field for the active environment.
// If namespace is empty, sets the env-level default. Otherwise sets a namespace override.
func SetContentField(field, namespace string) error {
	cfg := Load()
	if cfg.Active == "" {
		return fmt.Errorf("no active environment")
	}
	env := cfg.Envs[cfg.Active]
	if namespace == "" {
		env.ContentField = field
	} else {
		if env.ContentFields == nil {
			env.ContentFields = make(map[string]string)
		}
		if field == "" {
			delete(env.ContentFields, namespace)
		} else {
			env.ContentFields[namespace] = field
		}
	}
	cfg.Envs[cfg.Active] = env
	return Save(cfg)
}

// MaskKey masks an API key, showing only the first 8 characters.
func MaskKey(key string) string {
	if len(key) <= 8 {
		return key
	}
	return key[:8] + "..."
}
