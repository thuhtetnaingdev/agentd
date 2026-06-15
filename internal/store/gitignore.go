package store

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureGitignore checks if .gitignore exists in the project directory and adds
// the .agentd entry if not already present. Safe to call multiple times.
func EnsureGitignore(projectDir string) error {
	gitignorePath := filepath.Join(projectDir, ".gitignore")

	// Read existing entries
	existing := map[string]bool{}
	f, err := os.Open(gitignorePath)
	if err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				existing[line] = true
			}
		}
		f.Close()
	}

	// Check if already ignored
	if existing[".agentd"] || existing[".agentd/"] {
		return nil
	}

	// Append .agentd entry
	flag := os.O_APPEND | os.O_WRONLY | os.O_CREATE
	f, err = os.OpenFile(gitignorePath, flag, 0644)
	if err != nil {
		return fmt.Errorf("open .gitignore: %w", err)
	}
	defer f.Close()

	// Add newline if file has content
	if len(existing) > 0 {
		// Check if last char is already a newline
		info, _ := f.Stat()
		if info.Size() > 0 {
			lastByte := make([]byte, 1)
			f.ReadAt(lastByte, info.Size()-1)
			if lastByte[0] != '\n' {
				f.WriteString("\n")
			}
		}
	}

	_, err = f.WriteString(".agentd/\n")
	return err
}
