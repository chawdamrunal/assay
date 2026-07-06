package inventory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
)

// hookEntry mirrors the Claude Code hook configuration shape.
type hookEntry struct {
	Matcher string `json:"matcher"`
	Hooks   []struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	} `json:"hooks"`
}

type hooksFile struct {
	Hooks map[string][]hookEntry `json:"hooks"`
}

// EnumerateHooksFromSettings returns one Item per (event × matcher) hook entry.
// The Item.Name is "<event>:<matcher>" when matcher is set, or just "<event>"
// when matcher is empty; commands are stored joined by " | ".
func EnumerateHooksFromSettings(settingsPath string) ([]Item, error) {
	raw, err := os.ReadFile(settingsPath) // #nosec G304 -- settingsPath is a known config-file location
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", settingsPath, err)
	}
	var h hooksFile
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, fmt.Errorf("parse %s: %w", settingsPath, err)
	}

	events := make([]string, 0, len(h.Hooks))
	for k := range h.Hooks {
		events = append(events, k)
	}
	sort.Strings(events)

	var items []Item
	for _, event := range events {
		for _, entry := range h.Hooks[event] {
			cmds := make([]string, 0, len(entry.Hooks))
			for _, hk := range entry.Hooks {
				cmds = append(cmds, hk.Command)
			}
			name := event
			if entry.Matcher != "" {
				name = event + ":" + entry.Matcher
			}
			items = append(items, Item{
				Name: name,
				Kind: KindHook,
				Metadata: map[string]string{
					"event":    event,
					"matcher":  entry.Matcher,
					"commands": strings.Join(cmds, " | "),
				},
			})
		}
	}
	return items, nil
}
