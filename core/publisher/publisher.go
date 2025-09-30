package publisher

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cobeo2004/pg-rmq-publisher/core/config"
	"github.com/cobeo2004/pg-rmq-publisher/core/postgres"
	"github.com/cobeo2004/pg-rmq-publisher/core/rabbitmq"
)

// Publisher orchestrates PostgreSQL listener and RabbitMQ publisher
type Publisher struct {
	config    *config.Config
	pgListen  *postgres.Listener
	rmqPub    *rabbitmq.Publisher
	ctx       context.Context
	cancel    context.CancelFunc
}

// New creates a new Publisher with hardware-aware worker pools
func New(cfg *config.Config) (*Publisher, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Log worker pool configuration
	log.Printf("Worker pool configuration: PostgreSQL=%d, RabbitMQ=%d", cfg.PgWorkerPool, cfg.RabbitMQWorkerPool)

	// Create PostgreSQL listener with configurable worker pool
	pgListener, err := postgres.NewListener(cfg.PgConnectionString, cfg.PgEvents, cfg.PgWorkerPool)
	if err != nil {
		cancel()
		return nil, err
	}

	// Create RabbitMQ publisher with configurable worker pool and durability
	rmqPublisher, err := rabbitmq.NewPublisher(cfg.RabbitMQURL, cfg.RabbitMQExchange, cfg.RabbitMQExType, cfg.RabbitMQExchangeDurable, cfg.RabbitMQWorkerPool)
	if err != nil {
		cancel()
		pgListener.Close()
		return nil, err
	}

	p := &Publisher{
		config:   cfg,
		pgListen: pgListener,
		rmqPub:   rmqPublisher,
		ctx:      ctx,
		cancel:   cancel,
	}

	// Set notification handler
	pgListener.SetHandler(p.handleNotification)

	return p, nil
}

// Start starts the publisher
func (p *Publisher) Start() error {
	// Start PostgreSQL listener
	if err := p.pgListen.Start(p.ctx); err != nil {
		return err
	}

	// Handle OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	log.Println("Publisher started, waiting for notifications...")

	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutdown signal received, closing connections...")
	p.cancel()

	return nil
}

// handleNotification handles PostgreSQL notifications by publishing to RabbitMQ
func (p *Publisher) handleNotification(channel, payload string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return p.rmqPub.Publish(ctx, channel, payload)
}

// Close closes all connections
func (p *Publisher) Close() error {
	p.cancel()

	if p.rmqPub != nil {
		if err := p.rmqPub.Close(); err != nil {
			log.Printf("Error closing RabbitMQ publisher: %v", err)
		}
	}

	if p.pgListen != nil {
		if err := p.pgListen.Close(); err != nil {
			log.Printf("Error closing PostgreSQL listener: %v", err)
		}
	}

	log.Println("Publisher closed")
	return nil
}