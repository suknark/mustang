package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// loadConfig applies ~/.mustang.conf ("flag = value" lines) to flags that
// were not set on the command line — so the .app bundle, which is launched
// without arguments, can still be configured.
func loadConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(home, ".mustang.conf"))
	if err != nil {
		return
	}
	fromCLI := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { fromCLI[f.Name] = true })
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !fromCLI[k] {
			if err := flag.Set(k, v); err != nil {
				log.Printf("config: %s: %v", k, err)
			}
		}
	}
}

// openLogFile mirrors trigger lines to ~/Library/Logs/mustang.log so the
// windowless .app can still be debugged.
func openLogFile() *os.File {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(home, "Library/Logs/mustang.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return f
}
