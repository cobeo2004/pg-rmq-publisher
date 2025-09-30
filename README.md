# PostgreSQL to RabbitMQ Publisher

A high-performance Go application that bridges PostgreSQL's LISTEN/NOTIFY mechanism with RabbitMQ message queues, enabling real-time event-driven architectures with hardware-aware concurrency optimization.

## Features

- **PostgreSQL LISTEN/NOTIFY Integration** - Real-time database event listening
- **RabbitMQ Publishing** - Reliable message publishing to configurable exchanges
- **Hardware-Aware Concurrency** - Automatic worker pool sizing based on CPU cores
- **High-Throughput Architecture** - Buffered channels with worker pools for non-blocking operations
- **Thread-Safe Reconnection** - Automatic reconnection with mutex-protected connection handling
- **Graceful Shutdown** - Clean termination with WaitGroups ensuring all workers finish
- **Flexible Configuration** - Environment variables, .env files, or CLI flags
- **Configurable Exchange Durability** - Control whether RabbitMQ exchanges survive broker restarts

## Architecture

```
PostgreSQL NOTIFY → Listener → Notification Channel (500 buffer)
                                      ↓
                              PostgreSQL Workers (auto-sized)
                                      ↓
                              Handler Callback
                                      ↓
                              Message Queue (1000 buffer)
                                      ↓
                              RabbitMQ Workers (auto-sized)
                                      ↓
                              RabbitMQ Exchange
```

## Installation

### Prerequisites

- Go 1.25.1 or higher
- PostgreSQL with LISTEN/NOTIFY support
- RabbitMQ server

### Build from Source

```bash
# Clone the repository
git clone <repository-url>
cd pg-rmq-publisher

# Install dependencies
go mod tidy

# Build the application
go build -o pg-rmq-publisher main.go
```

## Configuration

Configuration follows this priority order: **CLI flags > Environment variables > Defaults**

### Environment Variables

Create a `.env` file in the project root:

```env
# PostgreSQL Configuration
PG_CONNECTION_STRING="postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable"
PG_EVENTS="notification-actions,device-actions,recording-actions"
PG_WORKER_POOL=3  # Optional: auto-detects based on CPU if not set

# RabbitMQ Configuration
RABBITMQ_URL="amqp://user:password@localhost:5672/"
RABBITMQ_EXCHANGE="pg_events"
RABBITMQ_EXCHANGE_TYPE="topic"
RABBITMQ_EXCHANGE_DURABLE="true"  # Set to "true" or "1" for durable, "false" for non-durable
RABBITMQ_WORKER_POOL=5  # Optional: auto-detects based on CPU if not set
```

### CLI Flags

```bash
./pg-rmq-publisher \
  --pg-conn "postgresql://user:pass@localhost:5432/db" \
  --pg-events "event1,event2,event3" \
  --pg-workers 4 \
  --rmq-url "amqp://user:pass@localhost:5672/" \
  --rmq-exchange "my_exchange" \
  --rmq-exchange-type "topic" \
  --rmq-exchange-durable \
  --rmq-workers 8
```

### Configuration Options

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `--pg-conn`, `-p` | `PG_CONNECTION_STRING` | *required* | PostgreSQL connection string |
| `--pg-events`, `-e` | `PG_EVENTS` | *required* | Comma-separated list of PostgreSQL events to listen to |
| `--pg-workers` | `PG_WORKER_POOL` | `auto` | Number of PostgreSQL notification handler workers (0=auto) |
| `--rmq-url`, `-r` | `RABBITMQ_URL` | *required* | RabbitMQ connection URL |
| `--rmq-exchange`, `-x` | `RABBITMQ_EXCHANGE` | *required* | RabbitMQ exchange name |
| `--rmq-exchange-type`, `-t` | `RABBITMQ_EXCHANGE_TYPE` | `topic` | Exchange type (direct, fanout, topic, headers) |
| `--rmq-exchange-durable` | `RABBITMQ_EXCHANGE_DURABLE` | `false` | Make exchange durable (survives broker restart) |
| `--rmq-workers` | `RABBITMQ_WORKER_POOL` | `auto` | Number of RabbitMQ publisher workers (0=auto) |

## Hardware-Aware Worker Pools

The application automatically optimizes worker pools based on available CPU cores:

### Default Sizing Formula

- **PostgreSQL Workers**: `max(1, min(3, numCPU/2))`
  - 1 CPU → 1 worker
  - 4 CPUs → 2 workers
  - 8 CPUs → 3 workers (capped)

- **RabbitMQ Workers**: `max(2, min(5, numCPU))`
  - 1 CPU → 2 workers
  - 4 CPUs → 4 workers
  - 8+ CPUs → 5 workers (capped)

### Manual Override

Set worker pools manually for specific workloads:

```bash
# High-throughput setup (override auto-detection)
PG_WORKER_POOL=10 RABBITMQ_WORKER_POOL=20 ./pg-rmq-publisher

# Low-resource setup
./pg-rmq-publisher --pg-workers 1 --rmq-workers 2
```

## Usage

### Basic Usage

```bash
# Using .env file (recommended)
./pg-rmq-publisher

# Using CLI flags
./pg-rmq-publisher \
  -p "postgresql://postgres:postgres@localhost:5432/db" \
  -e "event1,event2,event3" \
  -r "amqp://guest:guest@localhost:5672/" \
  -x "my_exchange"
```

### Running with Environment Variables

```bash
PG_CONNECTION_STRING="postgresql://user:pass@localhost:5432/db" \
PG_EVENTS="notifications,alerts" \
RABBITMQ_URL="amqp://user:pass@localhost:5672/" \
RABBITMQ_EXCHANGE="events" \
RABBITMQ_EXCHANGE_DURABLE="true" \
./pg-rmq-publisher
```

### Using Non-Durable Exchange

```bash
# Via .env
RABBITMQ_EXCHANGE_DURABLE="false" ./pg-rmq-publisher

# Via CLI
./pg-rmq-publisher --rmq-exchange-durable=false
```

## PostgreSQL Setup

### Create Notification Function

```sql
-- Create a function to send notifications
CREATE OR REPLACE FUNCTION notify_event()
RETURNS TRIGGER AS $$
BEGIN
  PERFORM pg_notify(
    'notification-actions',
    json_build_object(
      'operation', TG_OP,
      'table', TG_TABLE_NAME,
      'data', row_to_json(NEW)
    )::text
  );
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger
CREATE TRIGGER notify_on_insert
AFTER INSERT ON your_table
FOR EACH ROW
EXECUTE FUNCTION notify_event();
```

### Manual Testing

```sql
-- Send a test notification
SELECT pg_notify('notification-actions', '{"test": "message", "timestamp": "2025-09-30"}');
```

## RabbitMQ Setup

### Create Queue and Binding

```bash
# Access RabbitMQ management or use rabbitmqadmin

# Create a queue
rabbitmqadmin declare queue name=my_queue durable=true

# Bind queue to exchange with routing key
rabbitmqadmin declare binding source=pg_events destination=my_queue routing_key=notification-actions
```

### Routing Keys

The application uses the PostgreSQL event/channel name as the routing key. For example:
- Event `notification-actions` → Routing key `notification-actions`
- Event `device-actions` → Routing key `device-actions`

## Message Format

Messages published to RabbitMQ include:

```json
{
  "ContentType": "application/json",
  "Body": "<PostgreSQL notification payload>",
  "DeliveryMode": "Persistent",
  "Timestamp": "2025-09-30T17:00:00Z"
}
```

The message body contains the exact payload from PostgreSQL's `pg_notify()` call.

## Testing

### 1. Start Required Services

```bash
# PostgreSQL (Docker example)
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres

# RabbitMQ (Docker example)
docker run -d -p 5672:5672 -p 15672:15672 rabbitmq:3-management
```

### 2. Send Test Notification from PostgreSQL

```sql
-- Connect to PostgreSQL
psql "postgresql://postgres:postgres@localhost:5432/postgres"

-- Send notification
SELECT pg_notify('notification-actions', '{"user_id": 123, "action": "created"}');
```

### 3. Verify in RabbitMQ

Check the RabbitMQ management console at `http://localhost:15672` (guest/guest) to see the published message.

## Monitoring

### Application Logs

The application logs important events:

```
2025/09/30 17:00:00 Connected to PostgreSQL
2025/09/30 17:00:00 Worker pool configuration: PostgreSQL=3, RabbitMQ=5
2025/09/30 17:00:00 Connected to RabbitMQ, exchange 'pg_events' (type: topic, durable: true) ready
2025/09/30 17:00:00 Started 5 RabbitMQ publisher workers
2025/09/30 17:00:00 Started 3 PostgreSQL notification handler workers
2025/09/30 17:00:00 Listening to PostgreSQL event: notification-actions
2025/09/30 17:00:00 Publisher started, waiting for notifications...
2025/09/30 17:00:05 Received notification on channel 'notification-actions': {"test": "data"}
2025/09/30 17:00:05 Published message to exchange 'pg_events' with routing key 'notification-actions'
```

### Warning Messages

- **Queue Full**: `WARNING: Message queue full, dropping message` - Increase buffer size or worker count
- **Notification Queue Full**: `WARNING: Notification queue full, dropping notification` - Increase PostgreSQL workers
- **Connection Issues**: Automatic reconnection attempts with retry logging

## Error Handling

### Automatic Reconnection

- **RabbitMQ**: 5 retry attempts with 5-second delays
- **PostgreSQL**: Built-in `pq.Listener` reconnection with health checks every 90 seconds

### Graceful Shutdown

Press `Ctrl+C` or send `SIGTERM`:

```
2025/09/30 17:00:10 Shutdown signal received, closing connections...
2025/09/30 17:00:11 RabbitMQ publisher closed
2025/09/30 17:00:11 PostgreSQL listener closed
2025/09/30 17:00:11 Publisher closed
```

All workers complete processing before shutdown.

## Performance Tuning

### High-Throughput Scenarios

```env
# Increase worker pools
PG_WORKER_POOL=10
RABBITMQ_WORKER_POOL=20
```

To increase buffer sizes, modify:
- `core/rabbitmq/publisher.go:45` → `msgQueue` buffer (default: 1000)
- `core/postgres/listener.go:57` → `notifChan` buffer (default: 500)

### Low-Latency Scenarios

```env
# Reduce worker pools for lower overhead
PG_WORKER_POOL=1
RABBITMQ_WORKER_POOL=2
```

### Resource-Constrained Environments

```bash
# Use auto-detection (default) or set minimal workers
./pg-rmq-publisher --pg-workers 1 --rmq-workers 2
```

## Project Structure

```
pg-rmq-publisher/
├── core/
│   ├── config/
│   │   └── config.go          # Configuration management and validation
│   ├── postgres/
│   │   └── listener.go        # PostgreSQL LISTEN/NOTIFY handler with worker pool
│   ├── rabbitmq/
│   │   └── publisher.go       # RabbitMQ publisher with worker pool
│   └── publisher/
│       └── publisher.go       # Main orchestrator
├── main.go                    # CLI entry point
├── go.mod                     # Go module dependencies
├── .env                       # Environment configuration
└── README.md                  # This file
```

## Development

### Running Tests

```bash
go test ./...
```

### Development Mode

```bash
# Run without building
go run main.go
```

## Troubleshooting

### PostgreSQL Connection Issues

```
Error: failed to connect to PostgreSQL
```

**Solution**: Check connection string format and PostgreSQL accessibility:
```bash
psql "postgresql://postgres:postgres@localhost:5432/postgres"
```

### RabbitMQ Connection Issues

```
Error: failed to connect to RabbitMQ
```

**Solution**: Verify RabbitMQ is running and URL is correct:
```bash
rabbitmqctl status
```

### Syntax Error with Hyphenated Event Names

```
Error: pq: syntax error at or near "-"
```

**Solution**: Event names with hyphens are automatically quoted in LISTEN commands (fixed in latest version).

### Messages Not Appearing in RabbitMQ

**Check**:
1. Queue is bound to exchange with correct routing key
2. Exchange type matches routing strategy (use `topic` for wildcard routing)
3. PostgreSQL notifications are actually firing (test with `pg_notify()`)
4. Worker pools are not overloaded (check logs for queue full warnings)

## Dependencies

- `github.com/lib/pq` - PostgreSQL driver and LISTEN/NOTIFY support
- `github.com/rabbitmq/amqp091-go` - RabbitMQ AMQP client
- `github.com/joho/godotenv` - .env file loading
- `github.com/spf13/cobra` - CLI framework

## License

MIT