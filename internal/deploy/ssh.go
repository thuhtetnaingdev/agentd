package deploy

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHClient wraps an SSH connection to a remote VPS.
type SSHClient struct {
	Host     string
	Port     int
	Username string
	Password string
	client   *ssh.Client
}

// NewSSHClient creates a new SSH client for the given credentials.
func NewSSHClient(host string, port int, username, password string) *SSHClient {
	return &SSHClient{
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
	}
}

// Connect establishes an SSH connection.
func (c *SSHClient) Connect() error {
	config := &ssh.ClientConfig{
		User: c.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(c.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	addr := net.JoinHostPort(c.Host, fmt.Sprintf("%d", c.Port))
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}

	c.client = client
	return nil
}

// Close terminates the SSH connection.
func (c *SSHClient) Close() {
	if c.client != nil {
		c.client.Close()
	}
}

// Run executes a command on the remote host and returns stdout+stderr.
func (c *SSHClient) Run(cmd string) (string, error) {
	if c.client == nil {
		if err := c.Connect(); err != nil {
			return "", err
		}
	}

	session, err := c.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return string(output), fmt.Errorf("command failed: %w\n%s", err, string(output))
	}

	return string(output), nil
}

// RunWithInput executes a command and pipes input to stdin.
func (c *SSHClient) RunWithInput(cmd, stdin string) (string, error) {
	if c.client == nil {
		if err := c.Connect(); err != nil {
			return "", err
		}
	}

	session, err := c.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return "", err
	}

	go func() {
		defer stdinPipe.Close()
		stdinPipe.Write([]byte(stdin))
	}()

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return string(output), fmt.Errorf("command failed: %w\n%s", err, string(output))
	}

	return string(output), nil
}

// TestConnection verifies the SSH connection works.
func (c *SSHClient) TestConnection() error {
	output, err := c.Run("echo OK && uname -m && cat /etc/os-release | head -1")
	if err != nil {
		return err
	}
	_ = output
	return nil
}

// DetectArch returns the remote architecture (e.g., "x86_64", "aarch64").
func (c *SSHClient) DetectArch() (string, error) {
	output, err := c.Run("uname -m")
	if err != nil {
		return "", err
	}
	return output, nil
}
