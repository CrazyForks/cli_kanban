package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/happytaoer/cli_kanban/internal/db"
	"github.com/happytaoer/cli_kanban/internal/tui"
	"github.com/spf13/cobra"
)

var (
	workspace       string
	listWorkspaces  bool
	deleteWorkspace string
)

const (
	defaultWorkspace = "default"
	dataDirName      = ".cli_kanban"
	dbFilePrefix     = "cli_kanban__"
)

var workspaceNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cli_kanban",
		Short: "A terminal-based Kanban board",
		Long:  `cli_kanban is a beautiful TUI application for managing tasks in a Kanban board format.`,
		RunE:  runTUI,
	}

	rootCmd.PersistentFlags().StringVarP(&workspace, "workspace", "w", defaultWorkspace, "Workspace name (lowercase, digits, _, -)")
	rootCmd.PersistentFlags().BoolVarP(&listWorkspaces, "list", "l", false, "List available workspaces and exit")
	rootCmd.PersistentFlags().StringVarP(&deleteWorkspace, "delete", "d", "", "Delete a workspace database and exit")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runTUI(cmd *cobra.Command, args []string) error {
	if listWorkspaces && deleteWorkspace != "" {
		return errors.New("cannot use --list and --delete together")
	}

	if listWorkspaces {
		return listWorkspaceDatabases()
	}

	if deleteWorkspace != "" {
		return deleteWorkspaceDatabase(deleteWorkspace)
	}

	ws := workspace
	if ws == "" {
		ws = defaultWorkspace
	}
	if !workspaceNameRe.MatchString(ws) {
		return fmt.Errorf("invalid workspace name %q: must match %s", ws, workspaceNameRe.String())
	}

	dataDir, err := cliKanbanDataDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("failed to create data directory %q: %w", dataDir, err)
	}

	// One-time migration: copy old single-db default (~/.cli_kanban.db) into the new default workspace db.
	if ws == defaultWorkspace {
		oldPath, err := legacyDefaultDBPath()
		if err != nil {
			return err
		}
		newPath := filepath.Join(dataDir, dbFilePrefix+defaultWorkspace+".db")
		if err := migrateLegacyDefaultDB(oldPath, newPath); err != nil {
			return err
		}
	}

	dbPath := filepath.Join(dataDir, dbFilePrefix+ws+".db")

	// Initialize database
	database, err := db.New(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer database.Close()

	// Create TUI model
	model := tui.NewModel(database)

	// Start TUI
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	return nil
}

func deleteWorkspaceDatabase(ws string) error {
	if !workspaceNameRe.MatchString(ws) {
		return fmt.Errorf("invalid workspace name %q: must match %s", ws, workspaceNameRe.String())
	}

	dataDir, err := cliKanbanDataDir()
	if err != nil {
		return err
	}
	dbPath := filepath.Join(dataDir, dbFilePrefix+ws+".db")

	if err := os.Remove(dbPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("workspace %q not found", ws)
		}

		return fmt.Errorf("failed to delete workspace %q: %w", ws, err)
	}

	fmt.Printf("Deleted workspace %s\t%s\n", ws, dbPath)
	return nil
}

func listWorkspaceDatabases() error {
	dataDir, err := cliKanbanDataDir()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("No workspaces found.")
			return nil
		}
		return fmt.Errorf("failed to read data directory %q: %w", dataDir, err)
	}

	workspaces := make([]string, 0, len(entries))
	pathsByWorkspace := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, dbFilePrefix) || !strings.HasSuffix(name, ".db") {
			continue
		}
		ws := strings.TrimSuffix(strings.TrimPrefix(name, dbFilePrefix), ".db")
		if ws == "" {
			continue
		}
		workspaces = append(workspaces, ws)
		pathsByWorkspace[ws] = filepath.Join(dataDir, name)
	}

	sort.Strings(workspaces)
	if len(workspaces) == 0 {
		fmt.Println("No workspaces found.")
		return nil
	}
	for _, ws := range workspaces {
		fmt.Printf("%s\t%s\n", ws, pathsByWorkspace[ws])
	}
	return nil
}

func cliKanbanDataDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine user home directory: %w", err)
	}

	if homeDir == "" {
		return "", errors.New("failed to determine user home directory")
	}

	return filepath.Join(homeDir, dataDirName), nil
}

func legacyDefaultDBPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine user home directory: %w", err)
	}
	if homeDir == "" {
		return "", errors.New("failed to determine user home directory")
	}
	return filepath.Join(homeDir, ".cli_kanban.db"), nil
}

func migrateLegacyDefaultDB(oldPath, newPath string) error {
	if fileExists(newPath) {
		return nil
	}
	if !fileExists(oldPath) {
		return nil
	}
	return copyFile(oldPath, newPath, 0o600)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open legacy db %q: %w", src, err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("failed to create db %q: %w", dst, err)
	}
	defer func() {
		_ = dstFile.Close()
	}()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("failed to copy legacy db to %q: %w", dst, err)
	}
	if err := dstFile.Sync(); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("failed to sync db %q: %w", dst, err)
	}
	if err := dstFile.Close(); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("failed to close db %q: %w", dst, err)
	}
	return nil
}
