package climenu

import (
	"fmt"
	"os"
	"strconv"

	"github.com/charmbracelet/huh"
)

// Item is one menu choice (Value returned; Label shown).
type Item struct {
	Value string
	Label string
}

// IsTTY reports whether stdin is an interactive terminal.
func IsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// RequireTTY errors when stdin is not a TTY (scripts must pass flags/args).
func RequireTTY(hint string) error {
	if IsTTY() {
		return nil
	}
	if hint == "" {
		hint = "pass an explicit selector (non-interactive)"
	}
	return fmt.Errorf("%s", hint)
}

func accessible() bool {
	return os.Getenv("CAGE_ACCESSIBLE") != "" || os.Getenv("ACCESSIBLE") != ""
}

func numberedOptions(items []Item) []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(items))
	for i, it := range items {
		label := it.Label
		if label == "" {
			label = it.Value
		}
		opts = append(opts, huh.NewOption(strconv.Itoa(i+1)+") "+label, it.Value))
	}
	return opts
}

// Multi checkbox multi-select (space / ctrl+a). With CAGE_ACCESSIBLE=1, number toggle instead.
func Multi(title string, items []Item) ([]string, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no options")
	}
	if err := RequireTTY("pass explicit selectors (non-interactive)"); err != nil {
		return nil, err
	}
	opts := numberedOptions(items)
	var picked []string
	field := huh.NewMultiSelect[string]().
		Title(title).
		Description("space toggle · ctrl+a all · enter confirm · CAGE_ACCESSIBLE=1 for numbers").
		Options(opts...).
		Filterable(true).
		Value(&picked)
	form := huh.NewForm(huh.NewGroup(field)).WithAccessible(accessible())
	if err := form.Run(); err != nil {
		return nil, err
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("nothing selected")
	}
	return picked, nil
}

// One single-choice select (arrows / enter). With CAGE_ACCESSIBLE=1, type a number.
func One(title string, items []Item) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no options")
	}
	if err := RequireTTY("pass an explicit selector (non-interactive)"); err != nil {
		return "", err
	}
	opts := numberedOptions(items)
	var picked string
	field := huh.NewSelect[string]().
		Title(title).
		Description("↑↓ · enter · / filter · CAGE_ACCESSIBLE=1 for numbers").
		Options(opts...).
		Value(&picked)
	form := huh.NewForm(huh.NewGroup(field)).WithAccessible(accessible())
	if err := form.Run(); err != nil {
		return "", err
	}
	if picked == "" {
		return "", fmt.Errorf("nothing selected")
	}
	return picked, nil
}
