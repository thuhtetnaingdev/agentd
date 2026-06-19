package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"agentd/internal/config"
	"agentd/internal/server"
	"agentd/internal/store"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Repo is the GitHub repository for update/install.
const Repo = "thuhtetnaingdev/agentd"

var (
	port    int
	workDir string
	open    bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "agentd",
		Short: "AI-powered DevOps agent",
		Long: `agentd runs a web dashboard for agentic DevOps operations.
It analyzes your project, connects to your VPS, and handles
deployment — powered by any OpenAI-compatible LLM.`,
		RunE: run,
	}

	rootCmd.Flags().IntVarP(&port, "port", "p", 3001, "Port for the web dashboard")
	rootCmd.Flags().StringVarP(&workDir, "dir", "d", ".", "Target project directory to manage")
	rootCmd.Flags().BoolVar(&open, "open", false, "Open browser on startup")

	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(updateCmd())
	rootCmd.AddCommand(uninstallCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// --- subcommands ---

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("agentd %s\n", Version)
		},
	}
}

func updateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update agentd to the latest release",
		Long:  "Downloads the latest binary from GitHub Releases and replaces the current installation.",
		RunE: runUpdate,
	}
}

func uninstallCmd() *cobra.Command {
	var purge bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall agentd",
		Long:  "Removes the agentd binary. Use --purge to also remove config and session data.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(purge)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "Also remove ~/.agentd config and .agentd session directories")
	return cmd
}

// --- run ---

func run(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	if workDir != "." {
		wd = workDir
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := store.EnsureGitignore(wd); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update .gitignore: %v\n", err)
	}

	sessionsDir := filepath.Join(wd, ".agentd", "sessions")
	sessionStore := store.NewSessionStore(sessionsDir)

	deploymentStore := store.NewDeploymentStore(wd)

	envStore, err := config.NewEnvStore(wd, cfg.Settings().EncryptKey)
	if err != nil {
		return fmt.Errorf("create env store: %w", err)
	}

	srv, err := server.New(server.Options{
		Port:            port,
		WorkDir:         wd,
		Config:          cfg,
		SessionStore:    sessionStore,
		DeploymentStore: deploymentStore,
		EnvStore:        envStore,
	})
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		srv.Shutdown()
	}()

	fmt.Printf("agentd dashboard → http://localhost:%d\n", port)
	fmt.Printf("Managing: %s\n", wd)

	if open {
		openBrowser(fmt.Sprintf("http://localhost:%d", port))
	}

	return srv.Start()
}

// --- update ---

func runUpdate(cmd *cobra.Command, args []string) error {
	currentPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find current binary: %w", err)
	}

	latestTag, err := fetchLatestTag()
	if err != nil {
		return fmt.Errorf("fetch latest version: %w", err)
	}

	if Version != "dev" && Version == latestTag {
		fmt.Printf("Already up to date (%s)\n", Version)
		return nil
	}

	asset := fmt.Sprintf("agentd_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}

	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", Repo, latestTag, asset)
	fmt.Printf("Downloading %s ...\n", url)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d — release %s may not have a binary for %s/%s",
			resp.StatusCode, latestTag, runtime.GOOS, runtime.GOARCH)
	}

	// Write to a temp file first, then atomically replace
	tmpFile, err := os.CreateTemp("", "agentd-update-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("download: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Replace current binary
	if err := os.Rename(tmpPath, currentPath); err != nil {
		// Fallback: copy if cross-device rename fails
		if err := copyFile(tmpPath, currentPath); err != nil {
			return fmt.Errorf("replace binary: %w", err)
		}
	}

	fmt.Printf("Updated to %s\n", latestTag)
	return nil
}

// --- uninstall ---

func runUninstall(purge bool) error {
	currentPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find current binary: %w", err)
	}

	fmt.Printf("Removing %s ...\n", currentPath)
	if err := os.Remove(currentPath); err != nil {
		return fmt.Errorf("remove binary: %w", err)
	}
	fmt.Println("agentd removed.")

	if purge {
		home, err := os.UserHomeDir()
		if err == nil {
			cfgDir := filepath.Join(home, ".agentd")
			if _, err := os.Stat(cfgDir); err == nil {
				fmt.Printf("Removing config: %s\n", cfgDir)
				os.RemoveAll(cfgDir)
			}
		}
		fmt.Println("Run 'rm -rf .agentd' in your project directories to remove session data.")
		fmt.Println("Or use --purge to attempt automatic cleanup of ~/.agentd.")
	}

	return nil
}

// --- helpers ---

func fetchLatestTag() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", Repo)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", fmt.Errorf("no tag_name in release")
	}
	return release.TagName, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	cmd.Start()
}
