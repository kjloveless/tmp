package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type tracksComponent struct {
	picker           filepicker.Model
	loadingDirectory bool
}

func newTracksComponent(fp filepicker.Model) tracksComponent {
	return tracksComponent{picker: fp}
}

func (tc tracksComponent) Init() tea.Cmd {
	return tc.picker.Init()
}

func (tc tracksComponent) View() string {
	return tc.picker.View()
}

func (tc tracksComponent) ViewWithHeight(height int) string {
	tc.setHeight(height)
	return tc.picker.View()
}

func (tc *tracksComponent) setHeight(height int) {
	if height < 0 {
		height = 0
	}
	tc.picker.AutoHeight = false
	tc.picker.SetHeight(height)
}

func (tc tracksComponent) pickerEntries() ([]os.DirEntry, error) {
	entries, err := os.ReadDir(tc.picker.CurrentDirectory)
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() == entries[j].IsDir() {
			return entries[i].Name() < entries[j].Name()
		}
		return entries[i].IsDir()
	})

	if tc.picker.ShowHidden {
		return entries, nil
	}

	visible := entries[:0]
	for _, entry := range entries {
		hidden, _ := filepicker.IsHidden(entry.Name())
		if !hidden {
			visible = append(visible, entry)
		}
	}
	return visible, nil
}

func (tc tracksComponent) canSelectPath(path string) bool {
	if len(tc.picker.AllowedTypes) == 0 {
		return true
	}

	for _, ext := range tc.picker.AllowedTypes {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

func isDirectory(entry os.DirEntry, path string) bool {
	info, err := entry.Info()
	if err != nil {
		return entry.IsDir()
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return entry.IsDir()
	}

	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return entry.IsDir()
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		return entry.IsDir()
	}
	return targetInfo.IsDir()
}

func (tc tracksComponent) selectedFilePath() (string, bool) {
	path := tc.picker.HighlightedPath()
	if path == "" {
		return "", false
	}

	entry, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	if entry.IsDir() || !tc.canSelectPath(path) {
		return "", false
	}
	return path, true
}

func (tc tracksComponent) selectedDirectoryPath() (string, bool) {
	path := tc.picker.HighlightedPath()
	if path == "" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	if !info.IsDir() {
		return "", false
	}
	return path, true
}

func (tc tracksComponent) selectedPath() (string, bool) {
	path := tc.picker.HighlightedPath()
	if path == "" {
		return "", false
	}
	return path, true
}

func (tc *tracksComponent) Update(msg tea.Msg) (tea.Cmd, string, bool) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && tc.loadingDirectory {
		if key.Matches(keyMsg, tc.picker.KeyMap.Open) ||
			key.Matches(keyMsg, tc.picker.KeyMap.Select) ||
			key.Matches(keyMsg, tc.picker.KeyMap.Back) {
			return nil, "", false
		}
	}

	var cmd tea.Cmd
	tc.picker, cmd = tc.picker.Update(msg)
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		return cmd, "", false
	}
	if cmd != nil {
		if _, ok := msg.(tea.KeyMsg); ok {
			tc.loadingDirectory = true
			cmd = tea.Sequence(cmd, func() tea.Msg { return dirLoadedMsg{} })
		}
	}

	didSelect, path := tc.picker.DidSelectFile(msg)
	return cmd, path, didSelect
}
