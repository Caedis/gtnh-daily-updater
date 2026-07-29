package versionstamp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caedis/gtnh-daily-updater/internal/fileutil"
	"github.com/caedis/gtnh-daily-updater/internal/logging"
)

const (
	mainMenuPath   = "config/txloader/load/mainmenu/version.txt"
	dreamcraftPath = "config/GTNewHorizons/dreamcraft.cfg"
	coreModPath    = "config/DreamCoreMod.properties"
	serverPropPath = "server.properties"
	instanceCfg    = "instance.cfg"

	dreamcraftKey = "S:ModPackVersion="
	coreModKey    = "displayedModpackVersion="
	motdKey       = "motd="
	nameKey       = "name="

	motdPrefix = "GT:New Horizons"
	namePrefix = "GTNH"
)

// Apply stamps the display version into the files DAXXL stamps when assembling
// a release. Missing files, missing keys and user-customized values are left
// alone, except config/DreamCoreMod.properties where a missing key is appended.
// The returned slice names the files actually changed.
func Apply(instanceDir, gameDir string, v DisplayVersion) ([]string, error) {
	inGame := func(rel string) string { return filepath.Join(gameDir, filepath.FromSlash(rel)) }

	targets := []struct {
		name string
		fn   func() (bool, error)
	}{
		{mainMenuPath, func() (bool, error) { return stampMainMenu(inGame(mainMenuPath), v) }},
		{dreamcraftPath, func() (bool, error) {
			return stampKey(inGame(dreamcraftPath), dreamcraftKey, v.Long, false, nil)
		}},
		{coreModPath, func() (bool, error) {
			return stampKey(inGame(coreModPath), coreModKey, v.Long, true, nil)
		}},
		{serverPropPath, func() (bool, error) {
			return stampKey(filepath.Join(instanceDir, serverPropPath), motdKey, motdPrefix+" "+v.Long, false, prefixGuard(motdPrefix))
		}},
		{instanceCfg, func() (bool, error) {
			return stampKey(filepath.Join(instanceDir, instanceCfg), nameKey, namePrefix+" "+v.Long, false, prefixGuard(namePrefix))
		}},
	}

	var stamped []string
	var errs []error
	for _, t := range targets {
		changed, err := t.fn()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if changed {
			stamped = append(stamped, t.name)
		}
	}
	return stamped, errors.Join(errs...)
}

// prefixGuard accepts a value only while it still looks like the one the pack
// ships, so custom MOTDs and instance names survive an update.
func prefixGuard(prefix string) func(string) bool {
	return func(current string) bool {
		return current == prefix || strings.HasPrefix(current, prefix+" ")
	}
}

func stampMainMenu(path string, v DisplayVersion) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logging.Debugf("Verbose: versionstamp skipping missing %s\n", path)
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", path, err)
	}

	// DAXXL writes this file whole, with no trailing newline.
	want := "GTNH " + v.Short
	if v.Date != "" {
		want += " (" + v.Date + ")"
	}
	if string(content) == want {
		return false, nil
	}
	if err := fileutil.WriteFileAtomic(path, []byte(want), fileMode(path)); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

func stampKey(path, key, value string, appendIfMissing bool, guard func(string) bool) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logging.Debugf("Verbose: versionstamp skipping missing %s\n", path)
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", path, err)
	}

	f := parseLines(string(content))
	i, current := f.findKey(key)
	switch {
	case i < 0 && !appendIfMissing:
		logging.Debugf("Verbose: versionstamp key %q not found in %s\n", key, path)
		return false, nil
	case i < 0:
		f.appendLine(key + value)
	default:
		if guard != nil && !guard(current) {
			logging.Debugf("Verbose: versionstamp leaving customized %q in %s\n", key, path)
			return false, nil
		}
		f.setKey(i, key, value)
	}

	updated := f.render()
	if updated == string(content) {
		return false, nil
	}
	if err := fileutil.WriteFileAtomic(path, []byte(updated), fileMode(path)); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}

// fileMode returns the file's current permissions, or 0644 if unknown.
func fileMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0o644
}
