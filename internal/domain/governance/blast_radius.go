package governance

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Blast radius analysis errors
var (
	ErrBlastRadiusTooLarge   = errors.New("blast radius exceeds allowed limit")
	ErrCriticalPathsAffected = errors.New("critical paths would be affected")
	ErrTooManyServices       = errors.New("too many services affected")
	ErrInvalidBlastConfig    = errors.New("invalid blast radius configuration")
)

// BlastRadiusResult represents the result of blast radius analysis
type BlastRadiusResult struct {
	TotalFilesAffected    int                 `json:"total_files_affected"`
	TotalLinesAffected    int                 `json:"total_lines_affected"`
	ServicesAffected      []string            `json:"services_affected"`
	CriticalPathsAffected []string            `json:"critical_paths_affected"`
	RiskScore             float64             `json:"risk_score"`
	RiskLevel             RiskLevel           `json:"risk_level"`
	Allowed               bool                `json:"allowed"`
	DenialReason          string              `json:"denial_reason,omitempty"`
	Warnings              []string            `json:"warnings,omitempty"`
	Recommendations       []string            `json:"recommendations,omitempty"`
	Analysis              BlastRadiusAnalysis `json:"analysis"`
	EvaluatedAt           time.Time           `json:"evaluated_at"`
}

// BlastRadiusAnalysis provides detailed breakdown
type BlastRadiusAnalysis struct {
	FilesByType        map[string]int   `json:"files_by_type"`
	FilesByDirectory   map[string]int   `json:"files_by_directory"`
	CriticalFilesCount int              `json:"critical_files_count"`
	TestFilesCount     int              `json:"test_files_count"`
	ConfigFilesCount   int              `json:"config_files_count"`
	DependencyImpact   DependencyImpact `json:"dependency_impact"`
}

// DependencyImpact tracks how changes affect dependencies
type DependencyImpact struct {
	DirectDependents     int      `json:"direct_dependents"`
	TransitiveDependents int      `json:"transitive_dependents"`
	AffectedPackages     []string `json:"affected_packages"`
}

// RiskLevel categorizes the blast radius risk
type RiskLevel string

const (
	RiskLevelLow      RiskLevel = "low"
	RiskLevelMedium   RiskLevel = "medium"
	RiskLevelHigh     RiskLevel = "high"
	RiskLevelCritical RiskLevel = "critical"
)

// BlastRadiusConfig contains configuration for blast radius analysis
type BlastRadiusConfig struct {
	// Maximum files that can be affected by a single action
	MaxFilesAffected int `json:"max_files_affected"`

	// Maximum lines that can be affected
	MaxLinesAffected int `json:"max_lines_affected"`

	// Maximum services that can be affected
	MaxServicesAffected int `json:"max_services_affected"`

	// Critical paths that trigger extra scrutiny
	CriticalPaths []string `json:"critical_paths"`

	// Patterns that are always protected
	ProtectedPatterns []string `json:"protected_patterns"`

	// Trust level overrides
	TrustLevelOverrides map[int]BlastRadiusLimits `json:"trust_level_overrides"`

	// Risk score thresholds
	RiskThresholds RiskThresholds `json:"risk_thresholds"`
}

// BlastRadiusLimits defines limits for a specific trust level
type BlastRadiusLimits struct {
	MaxFiles    int `json:"max_files"`
	MaxLines    int `json:"max_lines"`
	MaxServices int `json:"max_services"`
}

// RiskThresholds defines thresholds for risk levels
type RiskThresholds struct {
	LowMax    float64 `json:"low_max"`
	MediumMax float64 `json:"medium_max"`
	HighMax   float64 `json:"high_max"`
}

// DefaultBlastRadiusConfig returns default configuration
func DefaultBlastRadiusConfig() *BlastRadiusConfig {
	return &BlastRadiusConfig{
		MaxFilesAffected:    10,
		MaxLinesAffected:    500,
		MaxServicesAffected: 2,
		CriticalPaths: []string{
			"*.env*",
			"*secrets*",
			"*credentials*",
			"**/config/production*",
			"**/.git/*",
			"**/migrations/*",
			"**/security/*",
			"**/auth/*",
		},
		ProtectedPatterns: []string{
			"**/.env",
			"**/.env.*",
			"**/secrets.yaml",
			"**/secrets.json",
			"**/*.pem",
			"**/*.key",
		},
		TrustLevelOverrides: map[int]BlastRadiusLimits{
			1: {MaxFiles: 3, MaxLines: 100, MaxServices: 1},      // Observer
			2: {MaxFiles: 5, MaxLines: 200, MaxServices: 1},      // Contributor
			3: {MaxFiles: 10, MaxLines: 500, MaxServices: 2},     // Developer
			4: {MaxFiles: 25, MaxLines: 2000, MaxServices: 5},    // Maintainer
			5: {MaxFiles: 100, MaxLines: 10000, MaxServices: 10}, // Admin
		},
		RiskThresholds: RiskThresholds{
			LowMax:    0.3,
			MediumMax: 0.6,
			HighMax:   0.8,
		},
	}
}

// BlastRadiusAnalyzer performs blast radius analysis
type BlastRadiusAnalyzer struct {
	config *BlastRadiusConfig
	mu     sync.RWMutex
}

// NewBlastRadiusAnalyzer creates a new analyzer
func NewBlastRadiusAnalyzer(config *BlastRadiusConfig) *BlastRadiusAnalyzer {
	if config == nil {
		config = DefaultBlastRadiusConfig()
	}
	return &BlastRadiusAnalyzer{
		config: config,
	}
}

// ActionScope represents the scope of an action for blast radius analysis
type ActionScope struct {
	// Files affected by the action
	AffectedPaths []string `json:"affected_paths"`

	// Estimated lines changed
	LinesChanged int `json:"lines_changed"`

	// Services affected
	Services []string `json:"services"`

	// Action type
	ActionType string `json:"action_type"`

	// Trust level of the agent
	TrustLevel int `json:"trust_level"`

	// Additional metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Analyze performs blast radius analysis on an action scope
func (a *BlastRadiusAnalyzer) Analyze(ctx context.Context, scope ActionScope) (*BlastRadiusResult, error) {
	a.mu.RLock()
	config := a.config
	a.mu.RUnlock()

	result := &BlastRadiusResult{
		TotalFilesAffected:    len(scope.AffectedPaths),
		TotalLinesAffected:    scope.LinesChanged,
		ServicesAffected:      scope.Services,
		CriticalPathsAffected: make([]string, 0),
		Warnings:              make([]string, 0),
		Recommendations:       make([]string, 0),
		EvaluatedAt:           time.Now(),
		Analysis: BlastRadiusAnalysis{
			FilesByType:      make(map[string]int),
			FilesByDirectory: make(map[string]int),
		},
	}

	// Analyze file types and directories
	for _, path := range scope.AffectedPaths {
		ext := filepath.Ext(path)
		if ext == "" {
			ext = "no-extension"
		}
		result.Analysis.FilesByType[ext]++

		dir := filepath.Dir(path)
		result.Analysis.FilesByDirectory[dir]++

		// Count special file types
		if isTestFile(path) {
			result.Analysis.TestFilesCount++
		}
		if isConfigFile(path) {
			result.Analysis.ConfigFilesCount++
		}
		if a.isCriticalPath(path) {
			result.Analysis.CriticalFilesCount++
			result.CriticalPathsAffected = append(result.CriticalPathsAffected, path)
		}
	}

	// Check protected patterns
	for _, path := range scope.AffectedPaths {
		if a.isProtectedPath(path) {
			result.Allowed = false
			result.DenialReason = fmt.Sprintf("path %s matches protected pattern", path)
			result.RiskLevel = RiskLevelCritical
			result.RiskScore = 1.0
			return result, nil
		}
	}

	// Get limits based on trust level
	limits := a.getLimitsForTrustLevel(scope.TrustLevel)

	// Calculate risk score
	result.RiskScore = a.calculateRiskScore(scope, result, limits)
	result.RiskLevel = a.getRiskLevel(result.RiskScore)

	// Check against limits
	if len(scope.AffectedPaths) > limits.MaxFiles {
		result.Allowed = false
		result.DenialReason = fmt.Sprintf("blast radius of %d files exceeds limit of %d for trust level %d",
			len(scope.AffectedPaths), limits.MaxFiles, scope.TrustLevel)
		return result, nil
	}

	if scope.LinesChanged > limits.MaxLines {
		result.Allowed = false
		result.DenialReason = fmt.Sprintf("lines changed (%d) exceeds limit of %d for trust level %d",
			scope.LinesChanged, limits.MaxLines, scope.TrustLevel)
		return result, nil
	}

	if len(scope.Services) > limits.MaxServices {
		result.Allowed = false
		result.DenialReason = fmt.Sprintf("services affected (%d) exceeds limit of %d for trust level %d",
			len(scope.Services), limits.MaxServices, scope.TrustLevel)
		return result, nil
	}

	// Check critical paths with lower trust levels
	if len(result.CriticalPathsAffected) > 0 && scope.TrustLevel < 4 {
		result.Allowed = false
		result.DenialReason = fmt.Sprintf("critical paths affected (%v) requires trust level 4 or higher",
			result.CriticalPathsAffected)
		return result, nil
	}

	// Add warnings
	if result.RiskScore > config.RiskThresholds.MediumMax {
		result.Warnings = append(result.Warnings, "high risk score detected")
	}
	if len(result.CriticalPathsAffected) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d critical paths affected", len(result.CriticalPathsAffected)))
	}
	if result.Analysis.ConfigFilesCount > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d configuration files affected", result.Analysis.ConfigFilesCount))
	}

	// Add recommendations
	if len(scope.AffectedPaths) > 5 {
		result.Recommendations = append(result.Recommendations, "consider splitting changes into smaller commits")
	}
	if len(scope.Services) > 1 {
		result.Recommendations = append(result.Recommendations, "review cross-service dependencies before proceeding")
	}

	result.Allowed = true
	return result, nil
}

func (a *BlastRadiusAnalyzer) getLimitsForTrustLevel(trustLevel int) BlastRadiusLimits {
	if limits, ok := a.config.TrustLevelOverrides[trustLevel]; ok {
		return limits
	}
	// Default limits
	return BlastRadiusLimits{
		MaxFiles:    a.config.MaxFilesAffected,
		MaxLines:    a.config.MaxLinesAffected,
		MaxServices: a.config.MaxServicesAffected,
	}
}

func (a *BlastRadiusAnalyzer) calculateRiskScore(scope ActionScope, result *BlastRadiusResult, limits BlastRadiusLimits) float64 {
	var score float64

	// Factor 1: Files affected (40% weight)
	if limits.MaxFiles > 0 {
		fileRatio := float64(len(scope.AffectedPaths)) / float64(limits.MaxFiles)
		score += 0.4 * min(fileRatio, 1.0)
	}

	// Factor 2: Lines changed (20% weight)
	if limits.MaxLines > 0 {
		lineRatio := float64(scope.LinesChanged) / float64(limits.MaxLines)
		score += 0.2 * min(lineRatio, 1.0)
	}

	// Factor 3: Services affected (20% weight)
	if limits.MaxServices > 0 {
		serviceRatio := float64(len(scope.Services)) / float64(limits.MaxServices)
		score += 0.2 * min(serviceRatio, 1.0)
	}

	// Factor 4: Critical paths (20% weight)
	if len(result.CriticalPathsAffected) > 0 {
		score += 0.2 * min(float64(len(result.CriticalPathsAffected))/3.0, 1.0)
	}

	return min(score, 1.0)
}

func (a *BlastRadiusAnalyzer) getRiskLevel(score float64) RiskLevel {
	switch {
	case score <= a.config.RiskThresholds.LowMax:
		return RiskLevelLow
	case score <= a.config.RiskThresholds.MediumMax:
		return RiskLevelMedium
	case score <= a.config.RiskThresholds.HighMax:
		return RiskLevelHigh
	default:
		return RiskLevelCritical
	}
}

func (a *BlastRadiusAnalyzer) isCriticalPath(path string) bool {
	for _, pattern := range a.config.CriticalPaths {
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		// Also check with ** patterns (simplified)
		if strings.Contains(pattern, "**") {
			simplePattern := strings.ReplaceAll(pattern, "**", "*")
			if matched, _ := filepath.Match(simplePattern, path); matched {
				return true
			}
		}
	}
	return false
}

func (a *BlastRadiusAnalyzer) isProtectedPath(path string) bool {
	for _, pattern := range a.config.ProtectedPatterns {
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		if strings.Contains(pattern, "**") {
			simplePattern := strings.ReplaceAll(pattern, "**", "*")
			if matched, _ := filepath.Match(simplePattern, path); matched {
				return true
			}
		}
	}
	return false
}

// UpdateConfig updates the analyzer configuration
func (a *BlastRadiusAnalyzer) UpdateConfig(config *BlastRadiusConfig) error {
	if config == nil {
		return ErrInvalidBlastConfig
	}
	a.mu.Lock()
	a.config = config
	a.mu.Unlock()
	return nil
}

// GetConfig returns the current configuration
func (a *BlastRadiusAnalyzer) GetConfig() *BlastRadiusConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.config
}

// Helper functions

func isTestFile(path string) bool {
	name := filepath.Base(path)
	patterns := []string{
		"*_test.go",
		"*_test.py",
		"*.test.js",
		"*.test.ts",
		"*.spec.js",
		"*.spec.ts",
		"test_*.py",
	}
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
	}
	return strings.Contains(path, "/test/") || strings.Contains(path, "/tests/") || strings.Contains(path, "/__tests__/")
}

func isConfigFile(path string) bool {
	name := filepath.Base(path)
	configFiles := []string{
		"config.yaml", "config.yml", "config.json",
		".env", ".env.local", ".env.production",
		"settings.yaml", "settings.json",
		"application.yaml", "application.properties",
	}
	for _, cf := range configFiles {
		if name == cf {
			return true
		}
	}
	return strings.Contains(path, "/config/")
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
