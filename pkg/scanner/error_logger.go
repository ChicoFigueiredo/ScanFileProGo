package scanner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrorLogFileInfo represents metadata of a saved error log file on disk.
type ErrorLogFileInfo struct {
	FileName  string    `json:"fileName"`
	FilePath  string    `json:"filePath"`
	SizeBytes int64     `json:"sizeBytes"`
	ModTime   time.Time `json:"modTime"`
}

// DiskErrorLogger handles thread-safe buffered writing of scan and hash errors directly to disk.
type DiskErrorLogger struct {
	mu       sync.Mutex
	file     *os.File
	writer   *bufio.Writer
	filePath string
	closed   bool
}

// NewDiskErrorLogger creates a new error log file in the specified directory (defaults to "logs").
func NewDiskErrorLogger(logDir string) (*DiskErrorLogger, error) {
	if logDir == "" {
		logDir = "logs"
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("falha ao criar pasta de logs '%s': %w", logDir, err)
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("scan_errors_%s.log", timestamp)
	fullPath := filepath.Join(logDir, filename)

	f, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("falha ao abrir arquivo de log '%s': %w", fullPath, err)
	}

	logger := &DiskErrorLogger{
		file:     f,
		writer:   bufio.NewWriterSize(f, 64*1024), // 64KB buffer
		filePath: fullPath,
	}

	// Write session header
	header := fmt.Sprintf("================================================================================\n"+
		" ScanFile Pro - Log de Erros e Itens Bloqueados\n"+
		" Início da Sessão: %s\n"+
		"================================================================================\n\n",
		time.Now().Format("02/01/2006 15:04:05"))

	_, _ = logger.writer.WriteString(header)
	_ = logger.writer.Flush()

	return logger, nil
}

// Log writes an error entry to the log file in a thread-safe manner.
func (l *DiskErrorLogger) Log(phase, path, errorMsg string) {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed || l.writer == nil {
		return
	}

	nowStr := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("[%s] [%s] %s | ERRO: %s\n", nowStr, strings.ToUpper(phase), path, errorMsg)

	_, _ = l.writer.WriteString(line)
}

// Flush ensures all buffered data is written to disk.
func (l *DiskErrorLogger) Flush() {
	if l == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed || l.writer == nil {
		return
	}

	_ = l.writer.Flush()
}

// Close flushes and closes the underlying log file.
func (l *DiskErrorLogger) Close() error {
	if l == nil {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}

	l.closed = true
	if l.writer != nil {
		footer := fmt.Sprintf("\n================================================================================\n"+
			" Fim da Sessão de Varredura: %s\n"+
			"================================================================================\n",
			time.Now().Format("02/01/2006 15:04:05"))
		_, _ = l.writer.WriteString(footer)
		_ = l.writer.Flush()
	}

	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// GetFilePath returns the relative path of the active log file.
func (l *DiskErrorLogger) GetFilePath() string {
	if l == nil {
		return ""
	}
	return l.filePath
}

// ListDiskErrorLogs returns all error log files in the specified directory.
func ListDiskErrorLogs(logDir string) ([]ErrorLogFileInfo, error) {
	if logDir == "" {
		logDir = "logs"
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ErrorLogFileInfo{}, nil
		}
		return nil, err
	}

	var results []ErrorLogFileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, "scan_errors_") && strings.HasSuffix(name, ".log") {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			results = append(results, ErrorLogFileInfo{
				FileName:  name,
				FilePath:  filepath.Join(logDir, name),
				SizeBytes: info.Size(),
				ModTime:   info.ModTime(),
			})
		}
	}

	// Sort newest first
	sort.Slice(results, func(i, j int) bool {
		return results[i].ModTime.After(results[j].ModTime)
	})

	return results, nil
}
