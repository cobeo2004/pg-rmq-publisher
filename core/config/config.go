package config

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	PgConnectionString    string
	PgEvents              []string
	PgWorkerPool          int  // PostgreSQL notification handler workers
	RabbitMQURL           string
	RabbitMQExchange      string
	RabbitMQExType        string
	RabbitMQExchangeDurable bool // Exchange durability
	RabbitMQWorkerPool    int  // RabbitMQ publisher workers
}

// Load loads configuration from environment variables and merges with provided config
func Load(cliConfig *Config) (*Config, error) {
	// Load .env file if exists
	_ = godotenv.Load()

	config := &Config{
		PgConnectionString:      cliConfig.PgConnectionString,
		PgEvents:                cliConfig.PgEvents,
		PgWorkerPool:            cliConfig.PgWorkerPool,
		RabbitMQURL:             cliConfig.RabbitMQURL,
		RabbitMQExchange:        cliConfig.RabbitMQExchange,
		RabbitMQExType:          cliConfig.RabbitMQExType,
		RabbitMQExchangeDurable: cliConfig.RabbitMQExchangeDurable,
		RabbitMQWorkerPool:      cliConfig.RabbitMQWorkerPool,
	}

	// Override with environment variables if not set via CLI
	if config.PgConnectionString == "" {
		config.PgConnectionString = os.Getenv("PG_CONNECTION_STRING")
	}
	if len(config.PgEvents) == 0 {
		eventsStr := os.Getenv("PG_EVENTS")
		if eventsStr != "" {
			config.PgEvents = strings.Split(eventsStr, ",")
		}
	}
	if config.PgWorkerPool == 0 {
		if envVal := os.Getenv("PG_WORKER_POOL"); envVal != "" {
			if val, err := strconv.Atoi(envVal); err == nil && val > 0 {
				config.PgWorkerPool = val
			}
		}
	}
	if config.RabbitMQURL == "" {
		config.RabbitMQURL = os.Getenv("RABBITMQ_URL")
	}
	if config.RabbitMQExchange == "" {
		config.RabbitMQExchange = os.Getenv("RABBITMQ_EXCHANGE")
	}
	if config.RabbitMQExType == "" {
		config.RabbitMQExType = os.Getenv("RABBITMQ_EXCHANGE_TYPE")
	}
	if !config.RabbitMQExchangeDurable {
		if envVal := os.Getenv("RABBITMQ_EXCHANGE_DURABLE"); envVal != "" {
			config.RabbitMQExchangeDurable = envVal == "true" || envVal == "1"
		}
	}
	if config.RabbitMQWorkerPool == 0 {
		if envVal := os.Getenv("RABBITMQ_WORKER_POOL"); envVal != "" {
			if val, err := strconv.Atoi(envVal); err == nil && val > 0 {
				config.RabbitMQWorkerPool = val
			}
		}
	}

	// Validate configuration
	if err := Validate(config); err != nil {
		return nil, err
	}

	return config, nil
}

// Validate validates the configuration and sets hardware-aware defaults
func Validate(config *Config) error {
	if config.PgConnectionString == "" {
		return fmt.Errorf("PostgreSQL connection string is required")
	}
	if len(config.PgEvents) == 0 {
		return fmt.Errorf("at least one PostgreSQL event is required")
	}
	if config.RabbitMQURL == "" {
		return fmt.Errorf("RabbitMQ URL is required")
	}
	if config.RabbitMQExchange == "" {
		return fmt.Errorf("RabbitMQ exchange name is required")
	}
	if config.RabbitMQExType == "" {
		config.RabbitMQExType = "topic"
	}

	// Trim whitespace from events
	for i := range config.PgEvents {
		config.PgEvents[i] = strings.TrimSpace(config.PgEvents[i])
	}

	// Set hardware-aware worker pool defaults
	numCPU := runtime.NumCPU()

	// PostgreSQL workers: min(3, numCPU/2) with floor of 1
	if config.PgWorkerPool == 0 {
		config.PgWorkerPool = max(1, min(3, numCPU/2))
	}

	// RabbitMQ workers: min(5, numCPU) with floor of 2
	if config.RabbitMQWorkerPool == 0 {
		config.RabbitMQWorkerPool = max(2, min(5, numCPU))
	}

	return nil
}

// Helper functions for min/max
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}