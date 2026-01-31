package ini

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hightemp/phvm/internal/fsutil"
)

const (
	disabledSuffix = ".disabled"
)

// IniFile represents an ini file in conf.d.
type IniFile struct {
	Name     string
	Path     string
	Enabled  bool
	Priority int // From filename prefix like 10-, 20-
}

// ConfDManager manages ini files in conf.d directory.
type ConfDManager struct {
	confDPath string
}

// NewConfDManager creates a new ConfDManager.
func NewConfDManager(confDPath string) *ConfDManager {
	return &ConfDManager{confDPath: confDPath}
}

// List lists all ini files in conf.d.
func (m *ConfDManager) List() ([]IniFile, error) {
	entries, err := os.ReadDir(m.confDPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read conf.d: %w", err)
	}

	var files []IniFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".ini") && !strings.HasSuffix(name, ".ini"+disabledSuffix) {
			continue
		}

		file := IniFile{
			Path: filepath.Join(m.confDPath, name),
		}

		if strings.HasSuffix(name, disabledSuffix) {
			file.Name = strings.TrimSuffix(name, disabledSuffix)
			file.Enabled = false
		} else {
			file.Name = name
			file.Enabled = true
		}

		file.Priority = extractPriority(file.Name)
		files = append(files, file)
	}

	// Sort by priority, then by name
	sort.Slice(files, func(i, j int) bool {
		if files[i].Priority != files[j].Priority {
			return files[i].Priority < files[j].Priority
		}
		return files[i].Name < files[j].Name
	})

	return files, nil
}

// Enable enables an ini file.
func (m *ConfDManager) Enable(name string) error {
	// Normalize name
	name = normalizeIniName(name)

	enabledPath := filepath.Join(m.confDPath, name)
	disabledPath := filepath.Join(m.confDPath, name+disabledSuffix)

	// Check if already enabled
	if fsutil.Exists(enabledPath) {
		return nil // Already enabled
	}

	// Check if disabled version exists
	if !fsutil.Exists(disabledPath) {
		return fmt.Errorf("ini file not found: %s", name)
	}

	// Rename to enable
	if err := os.Rename(disabledPath, enabledPath); err != nil {
		return fmt.Errorf("enable ini file: %w", err)
	}

	return nil
}

// Disable disables an ini file.
func (m *ConfDManager) Disable(name string) error {
	// Normalize name
	name = normalizeIniName(name)

	enabledPath := filepath.Join(m.confDPath, name)
	disabledPath := filepath.Join(m.confDPath, name+disabledSuffix)

	// Check if already disabled
	if fsutil.Exists(disabledPath) {
		return nil // Already disabled
	}

	// Check if enabled version exists
	if !fsutil.Exists(enabledPath) {
		return fmt.Errorf("ini file not found: %s", name)
	}

	// Rename to disable
	if err := os.Rename(enabledPath, disabledPath); err != nil {
		return fmt.Errorf("disable ini file: %w", err)
	}

	return nil
}

// Create creates a new ini file.
func (m *ConfDManager) Create(name, content string, priority int) error {
	name = normalizeIniName(name)

	// Add priority prefix if not present and priority > 0
	if priority > 0 && !hasPriorityPrefix(name) {
		name = fmt.Sprintf("%02d-%s", priority, name)
	}

	path := filepath.Join(m.confDPath, name)

	// Ensure conf.d exists
	if err := fsutil.EnsureDir(m.confDPath); err != nil {
		return fmt.Errorf("create conf.d: %w", err)
	}

	if err := fsutil.AtomicWriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write ini file: %w", err)
	}

	return nil
}

// Remove removes an ini file.
func (m *ConfDManager) Remove(name string) error {
	name = normalizeIniName(name)

	enabledPath := filepath.Join(m.confDPath, name)
	disabledPath := filepath.Join(m.confDPath, name+disabledSuffix)

	// Try to remove both
	removed := false
	if fsutil.Exists(enabledPath) {
		if err := os.Remove(enabledPath); err != nil {
			return fmt.Errorf("remove ini file: %w", err)
		}
		removed = true
	}
	if fsutil.Exists(disabledPath) {
		if err := os.Remove(disabledPath); err != nil {
			return fmt.Errorf("remove disabled ini file: %w", err)
		}
		removed = true
	}

	if !removed {
		return fmt.Errorf("ini file not found: %s", name)
	}

	return nil
}

// Get returns an ini file by name.
func (m *ConfDManager) Get(name string) (*IniFile, error) {
	files, err := m.List()
	if err != nil {
		return nil, err
	}

	name = normalizeIniName(name)
	for _, f := range files {
		if f.Name == name {
			return &f, nil
		}
	}

	return nil, fmt.Errorf("ini file not found: %s", name)
}

// IsEnabled checks if an ini file is enabled.
func (m *ConfDManager) IsEnabled(name string) bool {
	name = normalizeIniName(name)
	enabledPath := filepath.Join(m.confDPath, name)
	return fsutil.Exists(enabledPath)
}

// extractPriority extracts the priority number from a filename like "10-opcache.ini".
func extractPriority(name string) int {
	if len(name) < 3 {
		return 99
	}

	// Check for XX- prefix
	if name[2] == '-' && name[0] >= '0' && name[0] <= '9' && name[1] >= '0' && name[1] <= '9' {
		return int(name[0]-'0')*10 + int(name[1]-'0')
	}

	return 99 // Default priority
}

// hasPriorityPrefix checks if a filename has a priority prefix.
func hasPriorityPrefix(name string) bool {
	if len(name) < 3 {
		return false
	}
	return name[2] == '-' && name[0] >= '0' && name[0] <= '9' && name[1] >= '0' && name[1] <= '9'
}

// normalizeIniName normalizes an ini filename.
func normalizeIniName(name string) string {
	// Remove any path components
	name = filepath.Base(name)

	// Remove .disabled suffix if present (before checking .ini)
	name = strings.TrimSuffix(name, disabledSuffix)

	// Ensure .ini extension
	if !strings.HasSuffix(name, ".ini") {
		name = name + ".ini"
	}

	return name
}

// CreateExtensionIni creates an ini file for an extension.
func (m *ConfDManager) CreateExtensionIni(extName, extFile string, priority int, isZend bool) error {
	var content string
	if isZend {
		content = fmt.Sprintf("; Extension %s\nzend_extension=%s\n", extName, extFile)
	} else {
		content = fmt.Sprintf("; Extension %s\nextension=%s\n", extName, extFile)
	}

	name := fmt.Sprintf("%02d-%s.ini", priority, extName)
	return m.Create(name, content, 0) // Priority already in name
}
