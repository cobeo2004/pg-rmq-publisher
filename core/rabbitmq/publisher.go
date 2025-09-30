package rabbitmq

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Message represents a message to be published
type Message struct {
	RoutingKey string
	Payload    string
	Ctx        context.Context
}

// Publisher represents a RabbitMQ publisher with concurrent message handling
type Publisher struct {
	url             string
	exchangeName    string
	exchangeType    string
	exchangeDurable bool // Exchange durability flag
	conn            *amqp.Connection
	channel         *amqp.Channel
	mu              sync.RWMutex // Protects conn and channel during reconnection
	msgQueue        chan Message // Buffered channel for async publishing
	workerPool      int          // Number of worker goroutines
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// NewPublisher creates a new RabbitMQ publisher with configurable worker pool
func NewPublisher(url, exchangeName, exchangeType string, durable bool, workerPool int) (*Publisher, error) {
	ctx, cancel := context.WithCancel(context.Background())

	p := &Publisher{
		url:             url,
		exchangeName:    exchangeName,
		exchangeType:    exchangeType,
		exchangeDurable: durable,
		msgQueue:        make(chan Message, 1000), // Buffer up to 1000 messages
		workerPool:      workerPool,
		ctx:             ctx,
		cancel:          cancel,
	}

	if err := p.connect(); err != nil {
		cancel()
		return nil, err
	}

	// Start worker pool
	p.startWorkers()

	return p, nil
}

// startWorkers starts the worker pool for concurrent message publishing
func (p *Publisher) startWorkers() {
	for i := 0; i < p.workerPool; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	log.Printf("Started %d RabbitMQ publisher workers", p.workerPool)
}

// worker processes messages from the queue
func (p *Publisher) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case msg, ok := <-p.msgQueue:
			if !ok {
				return
			}
			p.publishMessage(msg)
		}
	}
}

// connect establishes connection to RabbitMQ and declares exchange (thread-safe)
func (p *Publisher) connect() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}
	p.conn = conn

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open RabbitMQ channel: %w", err)
	}
	p.channel = ch

	// Declare exchange
	err = ch.ExchangeDeclare(
		p.exchangeName,
		p.exchangeType,
		p.exchangeDurable, // durable (configurable)
		false,             // auto-deleted
		false,             // internal
		false,             // no-wait
		nil,               // arguments
	)
	if err != nil {
		ch.Close()
		conn.Close()
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	log.Printf("Connected to RabbitMQ, exchange '%s' (type: %s, durable: %t) ready", p.exchangeName, p.exchangeType, p.exchangeDurable)
	return nil
}

// Publish enqueues a message for async publishing (non-blocking)
func (p *Publisher) Publish(ctx context.Context, routingKey, payload string) error {
	msg := Message{
		RoutingKey: routingKey,
		Payload:    payload,
		Ctx:        ctx,
	}

	select {
	case p.msgQueue <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.ctx.Done():
		return fmt.Errorf("publisher is shutting down")
	default:
		// Queue is full, log warning but don't block
		log.Printf("WARNING: Message queue full, dropping message with routing key '%s'", routingKey)
		return fmt.Errorf("message queue full")
	}
}

// publishMessage performs the actual publish operation (called by workers)
func (p *Publisher) publishMessage(msg Message) {
	p.mu.RLock()
	ch := p.channel
	p.mu.RUnlock()

	if ch == nil {
		log.Printf("Channel is nil, attempting reconnection")
		if err := p.Reconnect(); err != nil {
			log.Printf("Failed to reconnect: %v", err)
			return
		}
		p.mu.RLock()
		ch = p.channel
		p.mu.RUnlock()
	}

	err := ch.PublishWithContext(
		msg.Ctx,
		p.exchangeName, // exchange
		msg.RoutingKey, // routing key
		false,          // mandatory
		false,          // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         []byte(msg.Payload),
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
		},
	)

	if err != nil {
		log.Printf("Failed to publish message to RabbitMQ: %v", err)
		// Attempt to reconnect
		if err := p.Reconnect(); err != nil {
			log.Printf("Failed to reconnect to RabbitMQ: %v", err)
		}
		return
	}

	log.Printf("Published message to exchange '%s' with routing key '%s'", p.exchangeName, msg.RoutingKey)
}

// Reconnect attempts to reconnect to RabbitMQ (thread-safe)
func (p *Publisher) Reconnect() error {
	log.Println("Attempting to reconnect to RabbitMQ...")

	// Close old connections (lock acquired inside connect())
	p.mu.Lock()
	if p.channel != nil {
		p.channel.Close()
		p.channel = nil
	}
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
	p.mu.Unlock()

	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		if err := p.connect(); err == nil {
			log.Println("Successfully reconnected to RabbitMQ")
			return nil
		}
		log.Printf("Reconnection attempt %d/%d failed, retrying in 5 seconds...", i+1, maxRetries)
		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("failed to reconnect after %d attempts", maxRetries)
}

// Close closes the RabbitMQ connection and stops workers
func (p *Publisher) Close() error {
	// Signal shutdown
	p.cancel()

	// Close message queue to stop workers
	close(p.msgQueue)

	// Wait for all workers to finish
	p.wg.Wait()

	// Close connections
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.channel != nil {
		if err := p.channel.Close(); err != nil {
			log.Printf("Error closing RabbitMQ channel: %v", err)
		}
	}
	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			log.Printf("Error closing RabbitMQ connection: %v", err)
		}
	}

	log.Println("RabbitMQ publisher closed")
	return nil
}