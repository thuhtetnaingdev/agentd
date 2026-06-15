package main

import (
	"fmt"
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

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Resolve work directory
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	if workDir != "." {
		wd = workDir
	}

	// Load or create config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Ensure .agentd is in .gitignore
	if err := store.EnsureGitignore(wd); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not update .gitignore: %v\n", err)
	}

	// Session store lives inside the project directory
	sessionsDir := filepath.Join(wd, ".agentd", "sessions")
	sessionStore := store.NewSessionStore(sessionsDir)

	// Env store for encrypted environment variables
	envStore, err := config.NewEnvStore(wd, cfg.Settings().EncryptKey)
	if err != nil {
		return fmt.Errorf("create env store: %w", err)
	}

	srv, err := server.New(server.Options{
		Port:         port,
		WorkDir:      wd,
		Config:       cfg,
		SessionStore: sessionStore,
		EnvStore:     envStore,
	})
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	// Graceful shutdown
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
