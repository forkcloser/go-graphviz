package gvc

import (
	"os"
	"path/filepath"
	"runtime"
)

// fontSuffixes are the file types the raster renderer can load: TrueType,
// TrueType collections and OpenType.
var fontSuffixes = []string{".ttf", ".ttc", ".otf"}

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
