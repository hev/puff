package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyConfigMigration(t *testing.T) {
	oldDir, oldFile, oldLegacyDir, oldLegacyFile := configDir, configFile, legacyConfigDir, legacyConfigFile
	t.Cleanup(func() {
		configDir, configFile, legacyConfigDir, legacyConfigFile = oldDir, oldFile, oldLegacyDir, oldLegacyFile
	})
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "copy legacy", true: "preserve puff config"}[existing], func(t *testing.T) {
			root := t.TempDir()
			configDir = filepath.Join(root, ".puff")
			configFile = filepath.Join(configDir, "config.toml")
			legacyConfigDir = filepath.Join(root, ".tpuff")
			legacyConfigFile = filepath.Join(legacyConfigDir, "config.toml")
			if err := os.MkdirAll(legacyConfigDir, 0700); err != nil {
				t.Fatal(err)
			}
			legacy := []byte("active = 'legacy'\n[envs.legacy]\napi_key = 'test-only-key'\nregion = 'aws-us-east-1'\n")
			if err := os.WriteFile(legacyConfigFile, legacy, 0600); err != nil {
				t.Fatal(err)
			}
			if existing {
				if err := os.MkdirAll(configDir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(configFile, []byte("active = 'current'\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got := Load()
			want := "legacy"
			if existing {
				want = "current"
			}
			if got.Active != want {
				t.Fatalf("active environment = %q, want %q", got.Active, want)
			}
			if !existing && got.Envs["legacy"].APIKey != "test-only-key" {
				t.Fatal("legacy credential was not retained")
			}
			data, err := os.ReadFile(legacyConfigFile)
			if err != nil || string(data) != string(legacy) {
				t.Fatal("legacy config was changed")
			}
			info, err := os.Stat(configFile)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				t.Fatalf("config permissions = %o", info.Mode().Perm())
			}
		})
	}
}
