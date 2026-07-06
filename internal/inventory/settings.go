package inventory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// settingsPermsFile is the partial shape of settings.json relevant to
// permission grants and denies.
type settingsPermsFile struct {
	Permissions struct {
		Allow []string `json:"allow"`
		Deny  []string `json:"deny"`
	} `json:"permissions"`
}

// EnumerateSettingsFromFile returns a single Item summarizing the permission
// grants in settingsPath. Returns empty slice when the file is missing or
// declares no permissions.
func EnumerateSettingsFromFile(settingsPath string) ([]Item, error) {
	raw, err := os.ReadFile(settingsPath) // #nosec G304 -- settingsPath is a known config-file location
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", settingsPath, err)
	}
	var s settingsPermsFile
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", settingsPath, err)
	}
	if len(s.Permissions.Allow) == 0 && len(s.Permissions.Deny) == 0 {
		return nil, nil
	}
	return []Item{{
		Name:        filepath.Base(settingsPath),
		Kind:        KindSettings,
		LocalPath:   settingsPath,
		Permissions: s.Permissions.Allow,
		Metadata: map[string]string{
			"allow_count": strconv.Itoa(len(s.Permissions.Allow)),
			"deny_count":  strconv.Itoa(len(s.Permissions.Deny)),
		},
	}}, nil
}
