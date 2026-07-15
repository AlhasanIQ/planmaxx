package appdata

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestStateDirUsesXDGStateHome(t *testing.T) {
	got, err := stateDir("linux", env(map[string]string{"XDG_STATE_HOME": "/state"}), fixed("/home/user"), fixed("/config"), fixed("/cache"))
	if err != nil || got != filepath.Join("/state", "planmaxx") {
		t.Fatalf("state dir = %q, %v", got, err)
	}
}

func TestStateDirUsesUnixFallback(t *testing.T) {
	got, err := stateDir("freebsd", env(nil), fixed("/home/user"), fixed("/config"), fixed("/cache"))
	want := filepath.Join("/home/user", ".local", "state", "planmaxx")
	if err != nil || got != want {
		t.Fatalf("state dir = %q, %v, want %q", got, err, want)
	}
}

func TestStateDirRejectsRelativeXDGStateHome(t *testing.T) {
	_, err := stateDir("linux", env(map[string]string{"XDG_STATE_HOME": "relative"}), fixed("/home/user"), fixed("/config"), fixed("/cache"))
	if err == nil {
		t.Fatal("expected relative XDG_STATE_HOME to fail")
	}
}

func TestStateDirUsesApplicationSupportOnDarwin(t *testing.T) {
	got, err := stateDir("darwin", env(nil), fixed("/home/user"), fixed("/application-support"), fixed("/cache"))
	if err != nil || got != filepath.Join("/application-support", "PlanMaxx") {
		t.Fatalf("state dir = %q, %v", got, err)
	}
}

func TestStateDirUsesLocalAppDataOnWindows(t *testing.T) {
	got, err := stateDir("windows", env(map[string]string{"LOCALAPPDATA": `C:\Local`}), fixed("home"), fixed("roaming"), fixed("cache"))
	if err != nil || got != filepath.Join(`C:\Local`, "PlanMaxx") {
		t.Fatalf("state dir = %q, %v", got, err)
	}
}

func TestStateDirFallsBackToWindowsCacheDir(t *testing.T) {
	got, err := stateDir("windows", env(nil), fixed("home"), fixed("roaming"), fixed(`C:\Local`))
	if err != nil || got != filepath.Join(`C:\Local`, "PlanMaxx") {
		t.Fatalf("state dir = %q, %v", got, err)
	}
}

func env(values map[string]string) lookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func fixed(value string) dirFunc { return func() (string, error) { return value, nil } }

func failed(err error) dirFunc { return func() (string, error) { return "", err } }

func TestStateDirPropagatesResolverErrors(t *testing.T) {
	want := errors.New("no home")
	if _, err := stateDir("linux", env(nil), failed(want), fixed("config"), fixed("cache")); !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}
