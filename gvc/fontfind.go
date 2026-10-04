package gvc

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// fontSuffixes are the file types the raster renderer can load: TrueType,
// TrueType collections and OpenType.
var fontSuffixes = []string{".ttf", ".ttc", ".otf"}

// findFont resolves a font name the way a user names fonts in a graph:
// a path to an existing file is taken as is; otherwise the platform's font
// directories are walked for a file whose name, with or without its
// extension, is the requested one, case-insensitively.
//
// Pure Go, no build tags: a platform without known font directories (wasip1,
// js, plan9) finds nothing and the caller falls back to the embedded default
// face, which is the behaviour the dropped go-findfont dependency had.
func findFont(name string) (string, error) {
	if _, err := os.Stat(name); err == nil {
		return name, nil
	}

	want := strings.ToLower(filepath.Base(name))

	var wantBare string

	for _, suffix := range fontSuffixes {
		if strings.HasSuffix(want, suffix) {
			wantBare = strings.TrimSuffix(want, suffix)

			break
		}
	}

	for _, dir := range fontDirectories() {
		found := ""
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // an unreadable entry is skipped, not fatal
			}

			base := strings.ToLower(d.Name())
			if base == want || (wantBare != "" && base == wantBare) {
				found = path

				return fs.SkipAll
			}

			for _, suffix := range fontSuffixes {
				if base == want+suffix {
					found = path

					return fs.SkipAll
				}
			}

			return nil
		})

		if found != "" {
			return found, nil
		}
	}

	return "", errors.New("font not found: " + name)
}

// fontDirectories lists where the running platform keeps fonts, user
// directories first.
func fontDirectories() []string {
	home, _ := os.UserHomeDir()

	switch runtime.GOOS {
	case "darwin":
		return nonEmpty(
			filepath.Join(home, "Library", "Fonts"),
			"/Library/Fonts",
			"/System/Library/Fonts",
		)
	case "windows":
		var dirs []string
		if windir := os.Getenv("windir"); windir != "" {
			dirs = append(dirs, filepath.Join(windir, "Fonts"))
		}

		if local := os.Getenv("localappdata"); local != "" {
			dirs = append(dirs, filepath.Join(local, "Microsoft", "Windows", "Fonts"))
		}

		return dirs
	case "android":
		return []string{"/system/fonts"}
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris", "illumos", "aix":
		dirs := []string{filepath.Join(home, ".fonts")}
		if data := os.Getenv("XDG_DATA_HOME"); data != "" {
			dirs = append(dirs, filepath.Join(data, "fonts"))
		} else {
			dirs = append(dirs, filepath.Join(home, ".local", "share", "fonts"))
		}

		if data := os.Getenv("XDG_DATA_DIRS"); data != "" {
			for _, d := range filepath.SplitList(data) {
				dirs = append(dirs, filepath.Join(d, "fonts"))
			}
		} else {
			dirs = append(dirs, "/usr/local/share/fonts", "/usr/share/fonts")
		}

		return nonEmpty(dirs...)
	default:
		return nil
	}
}

// nonEmpty drops the entries an empty home directory would have reduced to a
// bare relative path.
func nonEmpty(dirs ...string) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		if filepath.IsAbs(d) {
			out = append(out, d)
		}
	}

	return out
}
