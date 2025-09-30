package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/lib/pq"
)

// Notification represents a PostgreSQL notification
type Notification struct {
	Channel string
	Payload string
}

// NotificationHandler is a callback function for handling PostgreSQL notifications
type NotificationHandler func(channel, payload string) error

// Listener represents a PostgreSQL LISTEN/NOTIFY listener with concurrent processing
type Listener struct {
	connString  string
	events      []string
	db          *sql.DB
	listener    *pq.Listener
	handler     NotificationHandler
	notifChan   chan Notification // Buffered channel for notifications
	workerPool  int               // Number of handler worker goroutines
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewListener creates a new PostgreSQL listener with configurable worker pool
func NewListener(connString string, events []string, workerPool int) (*Listener, error) {
	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	log.Println("Connected to PostgreSQL")

	ctx, cancel := context.WithCancel(context.Background())

	return &Listener{
		connString: connString,
		events:     events,
		db:         db,
		notifChan:  make(chan Notification, 500), // Buffer up to 500 notifications
		workerPool: workerPool,
		ctx:        ctx,
		cancel:     cancel,
	}, nil
}

// SetHandler sets the notification handler callback
func (l *Listener) SetHandler(handler NotificationHandler) {
	l.handler = handler
}

// Start starts listening for PostgreSQL notifications and worker pool
func (l *Listener) Start(ctx context.Context) error {
	// Setup LISTEN for all events
	for _, event := range l.events {
		_, err := l.db.Exec(fmt.Sprintf(`LISTEN "%s"`, event))
		if err != nil {
			return fmt.Errorf("failed to LISTEN to event '%s': %w", event, err)
		}
		log.Printf("Listening to PostgreSQL event: %s", event)
	}

	// Create pq.Listener
	l.listener = pq.NewListener(l.connString, 10*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			log.Printf("Listener error: %v", err)
		}
	})

	// Listen to all configured events
	for _, event := range l.events {
		if err := l.listener.Listen(event); err != nil {
			return fmt.Errorf("failed to listen to event '%s': %w", event, err)
		}
	}

	// Start worker pool for processing notifications
	l.startWorkers()

	// Start listening loop
	go l.listen(ctx)

	return nil
}

// startWorkers starts the worker pool for concurrent notification processing
func (l *Listener) startWorkers() {
	for i := 0; i < l.workerPool; i++ {
		l.wg.Add(1)
		go l.worker(i)
	}
	log.Printf("Started %d PostgreSQL notification handler workers", l.workerPool)
}

// worker processes notifications from the channel
func (l *Listener) worker(id int) {
	defer l.wg.Done()

	for {
		select {
		case <-l.ctx.Done():
			return
		case notif, ok := <-l.notifChan:
			if !ok {
				return
			}
			l.processNotification(notif)
		}
	}
}

// listen is the main listening loop (enqueues notifications to worker pool)
func (l *Listener) listen(ctx context.Context) {
	defer l.listener.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case notification := <-l.listener.Notify:
			if notification == nil {
				continue
			}
			l.enqueueNotification(notification)
		case <-time.After(90 * time.Second):
			// Ping to check connection
			if err := l.listener.Ping(); err != nil {
				log.Printf("Listener ping failed: %v", err)
				// Attempt reconnection
				time.Sleep(5 * time.Second)
			}
		}
	}
}

// enqueueNotification sends notification to worker pool (non-blocking)
func (l *Listener) enqueueNotification(notification *pq.Notification) {
	log.Printf("Received notification on channel '%s': %s", notification.Channel, notification.Extra)

	notif := Notification{
		Channel: notification.Channel,
		Payload: notification.Extra,
	}

	select {
	case l.notifChan <- notif:
		// Successfully enqueued
	case <-l.ctx.Done():
		// Shutting down
		return
	default:
		// Channel full, log warning
		log.Printf("WARNING: Notification queue full, dropping notification from channel '%s'", notification.Channel)
	}
}

// processNotification processes a notification (called by workers)
func (l *Listener) processNotification(notif Notification) {
	if l.handler != nil {
		if err := l.handler(notif.Channel, notif.Payload); err != nil {
			log.Printf("Handler error for channel '%s': %v", notif.Channel, err)
		}
	}
}

// Close closes the PostgreSQL connection and stops workers
func (l *Listener) Close() error {
	// Signal shutdown
	l.cancel()

	// Close notification channel to stop workers
	close(l.notifChan)

	// Wait for all workers to finish
	l.wg.Wait()

	// Close connections
	if l.listener != nil {
		if err := l.listener.Close(); err != nil {
			log.Printf("Error closing pq.Listener: %v", err)
		}
	}
	if l.db != nil {
		if err := l.db.Close(); err != nil {
			log.Printf("Error closing DB: %v", err)
		}
	}

	log.Println("PostgreSQL listener closed")
	return nil
}