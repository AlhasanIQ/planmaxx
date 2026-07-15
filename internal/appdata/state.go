package appdata

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// StateDir returns PlanMaxx's platform-native directory for durable, user-scoped
// application state. Review bundles are machine-local state rather than
// configuration: they contain canonical local paths and resumable history.
func StateDir() (string, error) {
	return stateDir(runtime.GOOS, os.LookupEnv, os.UserHomeDir, os.UserConfigDir, os.UserCacheDir)
}

type lookupEnv func(string) (string, bool)
type dirFunc func() (string, error)

func stateDir(goos string, getenv lookupEnv, homeDir, configDir, cacheDir dirFunc) (string, error) {
	switch goos {
	case "darwin":
		// os.UserConfigDir is ~/Library/Application Support on macOS, which is
		// also the native location for durable application-managed data.
		root, err := requiredDir(configDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(root, "PlanMaxx"), nil
	case "windows":
		if value, ok := getenv("LOCALAPPDATA"); ok && value != "" {
			return filepath.Join(value, "PlanMaxx"), nil
		}
		// os.UserCacheDir also resolves to LocalAppData on Windows. Keep the
		// fallback for constrained environments where the variable is hidden.
		root, err := requiredDir(cacheDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(root, "PlanMaxx"), nil
	default:
		if value, ok := getenv("XDG_STATE_HOME"); ok && value != "" {
			if !filepath.IsAbs(value) {
				return "", errors.New("XDG_STATE_HOME must be an absolute path")
			}
			return filepath.Join(value, "planmaxx"), nil
		}
		home, err := requiredDir(homeDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state", "planmaxx"), nil
	}
}

func requiredDir(resolve dirFunc) (string, error) {
	value, err := resolve()
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", errors.New("user application state directory is empty")
	}
	return value, nil
}
