package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultHTTPAddr        = ":8080"
	defaultShutdownTimeout = 10 * time.Second
	defaultWorkflowDir     = "./workflows"
	defaultMaxActiveRuns   = 8
	defaultMaxConcurrent   = 16
	defaultMaxNodesPerRun  = 4
	defaultRunTimeout      = 2 * time.Minute
)

// Config carries HTTP server settings and the workflow runtime limits.
type Config struct {
	HTTPAddr           string
	ShutdownTimeout    time.Duration
	WorkflowDir        string
	MaxActiveRuns      int
	MaxConcurrentNodes int
	MaxNodesPerRun     int
	RunTimeout         time.Duration
}

// Load reads configuration from environment variables and validates limits.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:           envOrDefault("HTTP_ADDR", defaultHTTPAddr),
		ShutdownTimeout:    defaultShutdownTimeout,
		WorkflowDir:        envOrDefault("WORKFLOW_DIR", defaultWorkflowDir),
		MaxActiveRuns:      defaultMaxActiveRuns,
		MaxConcurrentNodes: defaultMaxConcurrent,
		MaxNodesPerRun:     defaultMaxNodesPerRun,
		RunTimeout:         defaultRunTimeout,
	}

	if value := os.Getenv("SHUTDOWN_TIMEOUT"); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse SHUTDOWN_TIMEOUT: %w", err)
		}
		if timeout <= 0 {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT must be positive")
		}
		cfg.ShutdownTimeout = timeout
	}

	if err := setPositiveInt(&cfg.MaxActiveRuns, "MAX_ACTIVE_RUNS"); err != nil {
		return Config{}, err
	}
	if err := setPositiveInt(&cfg.MaxConcurrentNodes, "MAX_CONCURRENT_NODES"); err != nil {
		return Config{}, err
	}
	if err := setPositiveInt(&cfg.MaxNodesPerRun, "MAX_NODES_PER_RUN"); err != nil {
		return Config{}, err
	}

	if value := os.Getenv("RUN_TIMEOUT"); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse RUN_TIMEOUT: %w", err)
		}
		if timeout <= 0 {
			return Config{}, fmt.Errorf("RUN_TIMEOUT must be positive")
		}
		cfg.RunTimeout = timeout
	}

	return cfg, nil
}

func setPositiveInt(target *int, key string) error {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("parse %s: %w", key, err)
	}
	if parsed < 1 {
		return fmt.Errorf("%s must be greater than zero", key)
	}
	*target = parsed
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
