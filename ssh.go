package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SSHClient handles SSH connections (without SFTP)
type SSHClient struct {
	sshClient *ssh.Client
	host      string
}

// NewSSHClient creates a new SSH client
// host can be in format "user@host:port" or just "host" (uses SSH config)
func NewSSHClient(host string) (*SSHClient, error) {
	// Load SSH keys
	authMethods := []ssh.AuthMethod{}
	if keyAuth := publicKeyAuth(); keyAuth != nil {
		authMethods = append(authMethods, keyAuth)
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no SSH authentication methods available - please ensure SSH keys are mounted")
	}

	config := &ssh.ClientConfig{
		User:            parseUsername(host),
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	hostAddr := parseHostAddr(host)

	// Connect to SSH
	client, err := ssh.Dial("tcp", hostAddr, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH: %w", err)
	}

	return &SSHClient{
		sshClient: client,
		host:      host,
	}, nil
}

// Close closes the SSH connection
func (c *SSHClient) Close() error {
	if c.sshClient != nil {
		return c.sshClient.Close()
	}
	return nil
}

// WalkDirectory recursively walks through a remote directory using SSH
func (c *SSHClient) WalkDirectory(dir string) ([]string, error) {
	// Use find command to list all files
	cmd := fmt.Sprintf("find %s -type f", shellescape(dir))

	session, err := c.sshClient.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	output, err := session.Output(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to run find command: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	var files []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}

	return files, nil
}

// DownloadFile downloads a file from remote to local using cat over SSH
func (c *SSHClient) DownloadFile(remotePath, localPath string) error {
	// Use cat to stream file contents
	cmd := fmt.Sprintf("cat %s", shellescape(remotePath))

	session, err := c.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// Create local file
	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %w", err)
	}
	defer localFile.Close()

	// Stream remote file to local
	session.Stdout = localFile

	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}

	return localFile.Sync()
}

// UploadFile uploads a local file to remote using cat over SSH
func (c *SSHClient) UploadFile(localPath, remotePath string) error {
	// Open local file
	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file: %w", err)
	}
	defer localFile.Close()

	// Use cat to write file contents
	cmd := fmt.Sprintf("cat > %s", shellescape(remotePath))

	session, err := c.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// Stream local file to remote
	session.Stdin = localFile

	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}

	return nil
}

// FileExists checks if a file exists on the remote server
func (c *SSHClient) FileExists(remotePath string) (bool, error) {
	cmd := fmt.Sprintf("test -f %s && echo exists || echo notfound", shellescape(remotePath))

	session, err := c.sshClient.NewSession()
	if err != nil {
		return false, fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	output, err := session.Output(cmd)
	if err != nil {
		return false, fmt.Errorf("failed to check file existence: %w", err)
	}

	return strings.TrimSpace(string(output)) == "exists", nil
}

// CreateDirectory creates a directory on the remote server
func (c *SSHClient) CreateDirectory(remotePath string) error {
	cmd := fmt.Sprintf("mkdir -p %s", shellescape(remotePath))

	session, err := c.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	if err := session.Run(cmd); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	return nil
}

// parseUsername extracts username from host string
func parseUsername(host string) string {
	if strings.Contains(host, "@") {
		parts := strings.Split(host, "@")
		return parts[0]
	}
	return os.Getenv("USER") // Default to current user
}

// parseHostAddr extracts host:port from host string
func parseHostAddr(host string) string {
	// Remove username if present
	hostPart := host
	if strings.Contains(host, "@") {
		parts := strings.Split(host, "@")
		hostPart = parts[1]
	}

	// Add default port if not specified
	if !strings.Contains(hostPart, ":") {
		return hostPart + ":22"
	}

	return hostPart
}

// sshAgent returns an SSH auth method using the SSH agent
func sshAgent() ssh.AuthMethod {
	// Try to connect to SSH agent
	socket := os.Getenv("SSH_AUTH_SOCK")
	if socket == "" {
		// Try to load keys from default location
		return publicKeyAuth()
	}

	// For simplicity, we'll use public key auth
	return publicKeyAuth()
}

// publicKeyAuth loads SSH keys from standard locations
func publicKeyAuth() ssh.AuthMethod {
	// Try common key locations
	keyPaths := []string{
		filepath.Join(os.Getenv("HOME"), ".ssh", "nas_key"),
		filepath.Join(os.Getenv("HOME"), ".ssh", "id_ed25519"),
		filepath.Join(os.Getenv("HOME"), ".ssh", "id_rsa"),
	}

	var signers []ssh.Signer
	for _, keyPath := range keyPaths {
		key, err := os.ReadFile(keyPath)
		if err != nil {
			continue
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			continue
		}

		signers = append(signers, signer)
	}

	if len(signers) == 0 {
		log.Println("Warning: No SSH keys found")
		return nil
	}

	return ssh.PublicKeys(signers...)
}

// shellescape escapes a string for safe use in shell commands
func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
