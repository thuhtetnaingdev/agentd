package deploy

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DeployViaSSH uploads a local directory to a remote path using the Go SSH client.
// It creates a tar.gz stream, pipes it through an SSH session, and extracts on the remote side.
// No external tools (sshpass, rsync) required — pure Go.
func DeployViaSSH(client *SSHClient, localPath, remotePath string, exclude []string) (string, error) {
	if client.client == nil {
		if err := client.Connect(); err != nil {
			return "", fmt.Errorf("ssh connect: %w", err)
		}
	}

	// Ensure remote path exists (try without sudo first, fall back to sudo)
	if _, err := client.Run(fmt.Sprintf("mkdir -p %s", remotePath)); err != nil {
		if _, sudoErr := client.Run(fmt.Sprintf("sudo mkdir -p %s", remotePath)); sudoErr != nil {
			return "", fmt.Errorf("cannot create remote directory %s (mkdir: %v, sudo mkdir: %v)", remotePath, err, sudoErr)
		}
	}

	session, err := client.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	// Pipe tar.gz through SSH to remote extract
	remoteCmd := fmt.Sprintf("tar xzf - -C %s", remotePath)
	var remoteOut, remoteErr strings.Builder
	session.Stdout = &remoteOut
	session.Stderr = &remoteErr

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin pipe: %w", err)
	}

	if err := session.Start(remoteCmd); err != nil {
		return "", fmt.Errorf("start remote tar: %w", err)
	}

	// Build exclude set
	excludeSet := map[string]bool{
		"node_modules": true,
		".git":         true,
		".env":         true,
		".env.local":   true,
	}
	for _, e := range exclude {
		excludeSet[e] = true
	}

	// Create tar.gz stream and write to stdin
	gw := gzip.NewWriter(stdinPipe)
	tw := tar.NewWriter(gw)

	var totalFiles int
	err = filepath.Walk(localPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}

		relPath, err := filepath.Rel(localPath, path)
		if err != nil {
			return nil
		}
		if relPath == "." {
			return nil
		}

		// Check exclude
		parts := strings.Split(relPath, string(filepath.Separator))
		for _, p := range parts {
			if excludeSet[p] {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip files matching *.log
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".log") {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return nil
		}

		if !info.IsDir() && info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			io.Copy(tw, f)
			f.Close()
		}

		totalFiles++
		return nil
	})

	tw.Close()
	gw.Close()
	stdinPipe.Close()

	if err := session.Wait(); err != nil {
		// Combine stdout + stderr for the full picture
		remoteOutput := remoteOut.String()
		if remoteErrStr := remoteErr.String(); remoteErrStr != "" {
			if remoteOutput != "" {
				remoteOutput += "\n" + remoteErrStr
			} else {
				remoteOutput = remoteErrStr
			}
		}
		if remoteOutput == "" {
			remoteOutput = err.Error()
		}
		return remoteOutput, fmt.Errorf("remote extract failed: %w", err)
	}

	return fmt.Sprintf("✓ Deployed %d files to %s", totalFiles, remotePath), nil
}

// RsyncDeploy tries rsync (key-based SSH first, no password).
// Falls back to DeployViaSSH if rsync is unavailable.
func RsyncDeploy(localPath, remoteUser, remoteHost string, remotePort int, remotePath, projectName string, exclude []string) (string, error) {
	// Try rsync first (uses default SSH key)
	excludeArgs := buildExcludeArgs(exclude)

	srcPath := localPath
	if !strings.HasSuffix(srcPath, "/") {
		srcPath += "/"
	}

	remoteAddr := fmt.Sprintf("%s@%s:%s", remoteUser, remoteHost, filepath.Join(remotePath, projectName))

	args := []string{
		"-avz",
		"--progress",
		"-e", fmt.Sprintf("ssh -p %d -o StrictHostKeyChecking=no -o BatchMode=yes -o ConnectTimeout=10", remotePort),
	}
	args = append(args, excludeArgs...)
	args = append(args, srcPath, remoteAddr)

	cmd := exec.Command("rsync", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return string(output), fmt.Errorf("rsync failed (try configuring SSH keys, or agentd will use its built-in transfer): %w\n%s", err, string(output))
	}

	return string(output), nil
}

// RsyncDeployWithPassword is deprecated — use DeployViaSSH instead.
// Kept for backward compatibility; falls back to rsync if sshpass is available.
func RsyncDeployWithPassword(localPath, remoteUser, remoteHost string, remotePort int, remotePassword, remotePath, projectName string, exclude []string) (string, error) {
	// Check if sshpass is available
	if _, err := exec.LookPath("sshpass"); err != nil {
		// sshpass not installed — return a clear error telling the caller to use DeployViaSSH
		return "", fmt.Errorf("sshpass not installed. Use the built-in SSH transfer instead (no external tools needed)")
	}

	excludeArgs := buildExcludeArgs(exclude)

	srcPath := localPath
	if !strings.HasSuffix(srcPath, "/") {
		srcPath += "/"
	}

	remoteAddr := fmt.Sprintf("%s@%s:%s", remoteUser, remoteHost, filepath.Join(remotePath, projectName))
	sshPassCmd := fmt.Sprintf("sshpass -p '%s' ssh -p %d -o StrictHostKeyChecking=no", remotePassword, remotePort)

	args := []string{"-avz", "--progress", "-e", sshPassCmd}
	args = append(args, excludeArgs...)
	args = append(args, srcPath, remoteAddr)

	cmd := exec.Command("rsync", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return string(output), fmt.Errorf("rsync failed: %w\n%s", err, string(output))
	}

	return string(output), nil
}

func buildExcludeArgs(exclude []string) []string {
	args := []string{
		"--exclude", "node_modules",
		"--exclude", ".git",
		"--exclude", ".env",
		"--exclude", ".env.local",
		"--exclude", "*.log",
	}
	for _, e := range exclude {
		args = append(args, "--exclude", e)
	}
	return args
}
