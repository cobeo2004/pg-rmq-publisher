package main

import (
	"log"

	"github.com/cobeo2004/pg-rmq-publisher/core/config"
	"github.com/cobeo2004/pg-rmq-publisher/core/publisher"

	"github.com/spf13/cobra"
)

func main() {
	var cliConfig config.Config

	rootCmd := &cobra.Command{
		Use:   "pg-rmq-publisher",
		Short: "PostgreSQL LISTEN/NOTIFY to RabbitMQ publisher",
		Long:  "Listens to PostgreSQL notification events and publishes them to RabbitMQ exchanges",
		Run: func(cmd *cobra.Command, args []string) {
			// Load configuration
			cfg, err := config.Load(&cliConfig)
			if err != nil {
				log.Fatalf("Configuration error: %v", err)
			}

			// Create publisher
			pub, err := publisher.New(cfg)
			if err != nil {
				log.Fatalf("Failed to create publisher: %v", err)
			}
			defer pub.Close()

			// Start publisher
			if err := pub.Start(); err != nil {
				log.Fatalf("Failed to start publisher: %v", err)
			}
		},
	}

	// Define CLI flags
	rootCmd.Flags().StringVarP(&cliConfig.PgConnectionString, "pg-conn", "p", "", "PostgreSQL connection string")
	rootCmd.Flags().StringSliceVarP(&cliConfig.PgEvents, "pg-events", "e", []string{}, "Comma-separated list of PostgreSQL events to listen to")
	rootCmd.Flags().IntVar(&cliConfig.PgWorkerPool, "pg-workers", 0, "Number of PostgreSQL notification handler workers (0=auto-detect based on CPU)")
	rootCmd.Flags().StringVarP(&cliConfig.RabbitMQURL, "rmq-url", "r", "", "RabbitMQ connection URL")
	rootCmd.Flags().StringVarP(&cliConfig.RabbitMQExchange, "rmq-exchange", "x", "", "RabbitMQ exchange name")
	rootCmd.Flags().StringVarP(&cliConfig.RabbitMQExType, "rmq-exchange-type", "t", "topic", "RabbitMQ exchange type (direct, fanout, topic, headers)")
	rootCmd.Flags().BoolVar(&cliConfig.RabbitMQExchangeDurable, "rmq-exchange-durable", false, "Make RabbitMQ exchange durable")
	rootCmd.Flags().IntVar(&cliConfig.RabbitMQWorkerPool, "rmq-workers", 0, "Number of RabbitMQ publisher workers (0=auto-detect based on CPU)")

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}