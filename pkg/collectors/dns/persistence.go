package dns

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

// BaselinePersistence handles saving and loading of learned baselines
type BaselinePersistence struct {
	logger  *zap.Logger
	dataDir string
	enabled bool
}

// NewBaselinePersistence creates a new persistence handler
func NewBaselinePersistence(logger *zap.Logger, dataDir string, enabled bool) *BaselinePersistence {
	if enabled {
		// Ensure data directory exists
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			logger.Error("Failed to create data directory, disabling persistence",
				zap.Error(err),
				zap.String("dir", dataDir))
			enabled = false
		}
	}

	return &BaselinePersistence{
		logger:  logger,
		dataDir: dataDir,
		enabled: enabled,
	}
}

// SaveBaselines saves learned baselines to disk
func (p *BaselinePersistence) SaveBaselines(baselines map[string]*DNSBaseline) error {
	if !p.enabled {
		return nil
	}

	filename := filepath.Join(p.dataDir, "dns_baselines.json")
	tmpFile := filename + ".tmp"

	// PersistenceData represents the structure for saving DNS baselines
	type PersistenceData struct {
		Version   string                  `json:"version"`
		SavedAt   time.Time               `json:"saved_at"`
		Baselines map[string]*DNSBaseline `json:"baselines"`
	}

	// Prepare serializable data
	saveData := PersistenceData{
		Version:   "1.0",
		SavedAt:   time.Now(),
		Baselines: baselines,
	}

	// Write to temporary file first
	file, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(saveData); err != nil {
		return fmt.Errorf("failed to encode baselines: %w", err)
	}

	// Atomic replace
	if err := os.Rename(tmpFile, filename); err != nil {
		os.Remove(tmpFile) // Cleanup temp file
		return fmt.Errorf("failed to replace baseline file: %w", err)
	}

	p.logger.Info("Saved DNS baselines to disk",
		zap.String("file", filename),
		zap.Int("baseline_count", len(baselines)))

	return nil
}

// LoadBaselines loads baselines from disk
func (p *BaselinePersistence) LoadBaselines() (map[string]*DNSBaseline, error) {
	if !p.enabled {
		return make(map[string]*DNSBaseline), nil
	}

	filename := filepath.Join(p.dataDir, "dns_baselines.json")

	// Check if file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		p.logger.Info("No saved baselines found, starting fresh")
		return make(map[string]*DNSBaseline), nil
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open baselines file: %w", err)
	}
	defer file.Close()

	var saveData struct {
		Version   string                  `json:"version"`
		SavedAt   time.Time               `json:"saved_at"`
		Baselines map[string]*DNSBaseline `json:"baselines"`
	}

	if err := json.NewDecoder(file).Decode(&saveData); err != nil {
		return nil, fmt.Errorf("failed to decode baselines: %w", err)
	}

	// Check age of saved data
	age := time.Since(saveData.SavedAt)
	if age > 7*24*time.Hour { // 1 week
		p.logger.Warn("Saved baselines are old, starting fresh",
			zap.Duration("age", age))
		return make(map[string]*DNSBaseline), nil
	}

	p.logger.Info("Loaded DNS baselines from disk",
		zap.String("file", filename),
		zap.Int("baseline_count", len(saveData.Baselines)),
		zap.Duration("age", age))

	return saveData.Baselines, nil
}

// SchedulePeriodicSave starts a goroutine that periodically saves baselines
func (p *BaselinePersistence) SchedulePeriodicSave(engine *DNSLearningEngine, interval time.Duration) {
	if !p.enabled {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			// Get current baselines (need to implement getter in learning engine)
			baselines := engine.GetBaselines()
			if len(baselines) > 0 {
				if err := p.SaveBaselines(baselines); err != nil {
					p.logger.Error("Failed to save baselines periodically",
						zap.Error(err))
				}
			}
		}
	}()

	p.logger.Info("Started periodic baseline saving",
		zap.Duration("interval", interval))
}
