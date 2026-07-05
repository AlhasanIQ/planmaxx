package browser

import (
	"fmt"
	"os/exec"
	"runtime"
)

type process interface {
	Wait() error
}

var startProcess = func(name string, args ...string) (process, error) {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func Open(url string) error {
	return open(runtime.GOOS, url)
}

func open(goos string, url string) error {
	name, args := commandFor(goos, url)
	if name == "" {
		return fmt.Errorf("opening a browser is not supported on %s", goos)
	}
	process, err := startProcess(name, args...)
	if err != nil {
		return err
	}
	go func() {
		_ = process.Wait()
	}()
	return nil
}

func commandFor(goos string, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "linux":
		return "xdg-open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "", nil
	}
}
