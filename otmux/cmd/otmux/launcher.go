package main

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/vamsikartik01/osmite-RnD/otmux/internal/config"
)

// launcherCmd manages remote.launchers in the settings file, so installers
// never edit the JSON themselves:
//
//	otmux launcher                    list
//	otmux launcher set <kind> <path>  add or replace
//	otmux launcher rm <kind>          remove
func launcherCmd(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	cfg, err := config.Load()
	if err != nil && sub != "" && sub != "ls" && sub != "list" {
		// Saving over a file we couldn't read would lose its other settings.
		return fmt.Errorf("can't read %s, so not changing it: %v", config.Path(), err)
	}
	switch sub {
	case "", "ls", "list":
		if len(cfg.Remote.Launchers) == 0 {
			fmt.Printf("No remote launchers in %s.\n", config.Path())
			return nil
		}
		kinds := make([]string, 0, len(cfg.Remote.Launchers))
		for k := range cfg.Remote.Launchers {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		for _, k := range kinds {
			fmt.Printf("%s\t%s\n", k, cfg.Remote.Launchers[k])
		}
		return nil
	case "set":
		if len(args) != 2 {
			return errors.New("usage: otmux launcher set <kind> <absolute path>")
		}
		if err := config.CheckLauncher(args[0], args[1]); err != nil {
			return err
		}
		if cfg.Remote.Launchers == nil {
			cfg.Remote.Launchers = map[string]string{}
		}
		cfg.Remote.Launchers[args[0]] = args[1]
	case "rm", "remove":
		if len(args) != 1 {
			return errors.New("usage: otmux launcher rm <kind>")
		}
		if _, ok := cfg.Remote.Launchers[args[0]]; !ok {
			fmt.Fprintf(os.Stderr, "otmux: no launcher %q\n", args[0])
			return nil
		}
		delete(cfg.Remote.Launchers, args[0])
	default:
		return fmt.Errorf("unknown launcher command %q (ls, set, rm)", sub)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Saved %s. Runs pick it up on their next start; no restart needed.\n", config.Path())
	return nil
}
