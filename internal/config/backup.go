package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// MaxBackups is the maximum number of config backups to keep
	MaxBackups = 3

	// BackupSuffix is the file extension for backup files
	BackupSuffix = ".bak"
)

// BackupUserConfig creates a timestamped backup of the user config file.
// Returns the backup file path on success.
// If no user config exists, returns empty string and nil error.
func BackupUserConfig() (string, error) {
	configPath := GetUserConfigPath()

	// Check if config exists
	if !UserConfigExists() {
		return "", nil // No config to backup
	}

	// Generate timestamped backup filename
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s%s.%s", configPath, BackupSuffix, timestamp)

	// Read existing config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read config for backup: %w", err)
	}

	// Write backup
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write backup: %w", err)
	}

	// Clean up old backups (keep only MaxBackups)
	if err := cleanupOldBackups(configPath); err != nil {
		// Log but don't fail - backup was successful
		// The cleanup is best-effort
		_ = err
	}

	return backupPath, nil
}

// ListUserConfigBackups returns all backup files for the user config,
// sorted by modification time (newest first).
func ListUserConfigBackups() ([]string, error) {
	configPath := GetUserConfigPath()
	configDir := filepath.Dir(configPath)
	configBase := filepath.Base(configPath)

	// List all files in config directory
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No config dir = no backups
		}
		return nil, fmt.Errorf("failed to list config directory: %w", err)
	}

	// Filter backup files
	var backups []string
	prefix := configBase + BackupSuffix + "."
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), prefix) {
			backups = append(backups, filepath.Join(configDir, entry.Name()))
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		infoI, _ := os.Stat(backups[i])
		infoJ, _ := os.Stat(backups[j])
		if infoI == nil || infoJ == nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	return backups, nil
}

// cleanupOldBackups removes backups beyond MaxBackups, keeping the newest.
func cleanupOldBackups(configPath string) error {
	backups, err := ListUserConfigBackups()
	if err != nil {
		return err
	}

	// Keep only the newest MaxBackups
	if len(backups) <= MaxBackups {
		return nil
	}

	// Remove oldest backups
	for _, backup := range backups[MaxBackups:] {
		if err := os.Remove(backup); err != nil {
			// Best effort - continue removing others
			continue
		}
	}

	return nil
}

// RestoreUserConfig restores the user config from a backup file.
// The current config (if any) is backed up before restore.
func RestoreUserConfig(backupPath string) error {
	configPath := GetUserConfigPath()

	// Verify backup exists
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup file not found: %w", err)
	}

	// Backup current config before restore (if it exists)
	if UserConfigExists() {
		if _, err := BackupUserConfig(); err != nil {
			return fmt.Errorf("failed to backup current config before restore: %w", err)
		}
	}

	// Read backup
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	// Ensure config directory exists
	configDir := GetUserConfigDir()
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write restored config
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write restored config: %w", err)
	}

	return nil
}
