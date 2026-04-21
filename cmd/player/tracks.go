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
	selected         int
	stack            []int
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
	if height < 0 {
		height = 0
	}
	tc.picker.AutoHeight = false
	tc.picker.SetHeight(height)
	return tc.picker.View()
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
	entries, err := tc.pickerEntries()
	if err != nil || len(entries) == 0 || tc.selected < 0 || tc.selected >= len(entries) {
		return "", false
	}

	entry := entries[tc.selected]
	path := filepath.Join(tc.picker.CurrentDirectory, entry.Name())
	if isDirectory(entry, path) || !tc.canSelectPath(path) {
		return "", false
	}
	return path, true
}

func (tc *tracksComponent) clampSelected() {
	entries, err := tc.pickerEntries()
	if err != nil || len(entries) == 0 {
		tc.selected = 0
		return
	}

	switch {
	case tc.selected < 0:
		tc.selected = 0
	case tc.selected >= len(entries):
		tc.selected = len(entries) - 1
	}
}

func (tc *tracksComponent) syncSelection(msg tea.Msg, previousDirectory string) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		tc.clampSelected()
		return
	}

	if tc.picker.CurrentDirectory != previousDirectory {
		if key.Matches(keyMsg, tc.picker.KeyMap.Back) {
			if len(tc.stack) > 0 {
				last := len(tc.stack) - 1
				tc.selected = tc.stack[last]
				tc.stack = tc.stack[:last]
			} else {
				tc.selected = 0
			}
		} else {
			tc.stack = append(tc.stack, tc.selected)
			tc.selected = 0
		}
		tc.clampSelected()
		return
	}

	entries, err := tc.pickerEntries()
	if err != nil || len(entries) == 0 {
		tc.selected = 0
		return
	}

	switch {
	case key.Matches(keyMsg, tc.picker.KeyMap.GoToTop):
		tc.selected = 0
	case key.Matches(keyMsg, tc.picker.KeyMap.GoToLast):
		tc.selected = len(entries) - 1
	case key.Matches(keyMsg, tc.picker.KeyMap.Down):
		tc.selected++
	case key.Matches(keyMsg, tc.picker.KeyMap.Up):
		tc.selected--
	case key.Matches(keyMsg, tc.picker.KeyMap.PageDown):
		tc.selected += tc.picker.Height()
	case key.Matches(keyMsg, tc.picker.KeyMap.PageUp):
		tc.selected -= tc.picker.Height()
	}
	tc.clampSelected()
}

func (tc *tracksComponent) Update(msg tea.Msg) (tea.Cmd, string, bool) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && tc.loadingDirectory {
		if key.Matches(keyMsg, tc.picker.KeyMap.Open) ||
			key.Matches(keyMsg, tc.picker.KeyMap.Select) ||
			key.Matches(keyMsg, tc.picker.KeyMap.Back) {
			return nil, "", false
		}
	}

	prevDir := tc.picker.CurrentDirectory
	var cmd tea.Cmd
	tc.picker, cmd = tc.picker.Update(msg)
	tc.syncSelection(msg, prevDir)
	if tc.picker.CurrentDirectory != prevDir && cmd != nil {
		tc.loadingDirectory = true
		cmd = tea.Sequence(cmd, func() tea.Msg { return dirLoadedMsg{} })
	}

	didSelect, path := tc.picker.DidSelectFile(msg)
	return cmd, path, didSelect
}
