# Burin

English | [简体中文](README.cn.md)

<p align="center">
  <strong>High-Performance Distributed Cache System</strong>
</p>

<p align="center">
  <a href="#features">Features</a> •
  <a href="#quick-start">Quick Start</a> •
  <a href="#architecture">Architecture</a> •
  <a href="#usage-examples">Usage Examples</a> •
  <a href="#configuration">Configuration</a> •
  <a href="#building">Building</a>
</p>

---

## Introduction

Burin is a high-performance distributed cache system that implements strong consistency based on the Raft consensus algorithm. It provides rich data structure support, transaction processing, geolocation queries, and is suitable for scenarios requiring high availability and data consistency.

## Features

### Core Features
- ✅ **Distributed Consistency**: Based on Raft consensus algorithm, ensuring strong data consistency
- ✅ **Multiple Data Structures**: Support for String, Hash, List, Set, Sorted Set, and more
- ✅ **Transaction Support**: ACID transaction processing capabilities
- ✅ **Geolocation Queries**: Built-in GeoHash support for efficient geolocation search
- ✅ **TTL Management**: Flexible expiration time control
- ✅ **Multiple Databases**: Support for multiple isolated logical databases
- ✅ **Authentication & Authorization**: Complete user authentication and permission management
- ✅ **Batch Operations**: Efficient batch read/write interfaces

### Advanced Features
- 🚀 **High Performance**: Uses BadgerDB as storage engine with optimized serialization protocol
- 🔄 **Failover**: Automatic node failure detection and recovery
- 📊 **Monitoring Metrics**: Built-in Prometheus metrics support
- 🔧 **Flexible Configuration**: YAML configuration file support
- 🛠️ **CLI Tool**: Feature-rich CLI and interactive client
- 📦 **Connection Pool**: Efficient connection pool management

## Quick Start

### Option 1: Using Docker (Recommended)

**Prerequisites**: Docker 20.10+ and Docker Compose V2

```bash
# Clone the repository
git clone https://github.com/qorm/burin.git
cd burin/docker

# Build and start the cluster
make build
make up-d

# Check status
make status

# View logs
make logs
```

For more Docker deployment information, see the [Docker Deployment Guide](./docker/README.md).

### Option 2: Local Build and Run

**Prerequisites**: Go 1.24.0 or higher

```bash
# Clone the repository
git clone https://github.com/qorm/burin.git
cd burin

# Build server
./build.sh

# Build CLI tool
./build-cli.sh
```

### Starting the Server

```bash
# Generate default configuration file
./build/burin-darwin-arm64 -generate-config

# Start node
./build/burin-darwin-arm64 -config burin.yaml
```

### Using the CLI

```bash
# Start interactive client
./build/burin-cli-darwin-arm64

# Connect to server
connect 127.0.0.1:8099

# Login (default username: burin, password: burin@secret)
login burin burin@secret

# Basic operations
set mykey "Hello Burin"
get mykey
del mykey
```

## Architecture

### System Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    Client Applications                   │
└───────────────────┬─────────────────────────────────────┘
                    │
        ┌───────────┴──────────┐
        │   Burin Client SDK   │
        └───────────┬──────────┘
                    │
┌───────────────────┴─────────────────────────────────────┐
│                    Burin Cluster                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐             │
│  │  Node 1  │  │  Node 2  │  │  Node 3  │             │
│  │ (Leader) │◄─┤(Follower)│◄─┤(Follower)│             │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘             │
│       │             │             │                     │
│  ┌────▼─────────────▼─────────────▼─────┐              │
│  │         Raft Consensus Layer         │              │
│  └────┬─────────────┬─────────────┬─────┘              │
│       │             │             │                     │
│  ┌────▼────┐   ┌────▼────┐   ┌────▼────┐              │
│  │ BadgerDB│   │ BadgerDB│   │ BadgerDB│              │
│  └─────────┘   └─────────┘   └─────────┘              │
└─────────────────────────────────────────────────────────┘
```

### Core Components

- **cProtocol**: Custom binary protocol supporting efficient client-server communication
- **Consensus**: Raft-based consensus layer ensuring data consistency
- **Store**: BadgerDB storage engine wrapper providing persistence capabilities
- **Transaction**: MVCC transaction manager
- **Business**: Business logic layer handling various data operations
- **Client**: Go client SDK providing clean API

## Usage Examples

### Basic Cache Operations

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/qorm/burin/client"
)

func main() {
    // Create configuration
    config := client.NewDefaultConfig()
    config.Connection.Endpoint = "localhost:8099"
    config.Auth.Username = "burin"
    config.Auth.Password = "burin@secret"
    
    // Create client
    burinClient, err := client.NewClient(config)
    if err != nil {
        panic(err)
    }
    
    // Connect and login
    if err := burinClient.Connect(); err != nil {
        panic(err)
    }
    defer burinClient.Disconnect()
    
    // Set cache with TTL
    err = burinClient.Set("user:1001", []byte(`{"name":"Alice","age":25}`), 
        client.WithTTL(5*time.Minute))
    if err != nil {
        panic(err)
    }
    
    // Get cache
    resp, err := burinClient.Get("user:1001")
    if err != nil {
        panic(err)
    }
    
    if resp.Found {
        fmt.Printf("Value: %s\n", string(resp.Value))
    }
    
    // Batch operations
    keys := []string{"key1", "key2", "key3"}
    values, err := burinClient.MGet(keys...)
    if err != nil {
        panic(err)
    }
    
    for key, value := range values {
        fmt.Printf("%s: %s\n", key, string(value))
    }
}
```

### Transaction Operations

```go
// Begin transaction
txn, err := burinClient.BeginTransaction()
if err != nil {
    panic(err)
}

// Operations within transaction
txn.Set("account:1", []byte("1000"))
txn.Set("account:2", []byte("2000"))

// Commit transaction
err = burinClient.CommitTransaction(txn.ID)
if err != nil {
    // Rollback transaction
    burinClient.RollbackTransaction(txn.ID)
    panic(err)
}
```

### Geolocation Queries

```go
// Add geolocations
err = burinClient.GeoAdd("locations", map[string]client.GeoPoint{
    "store1": {Latitude: 39.9042, Longitude: 116.4074}, // Beijing
    "store2": {Latitude: 31.2304, Longitude: 121.4737}, // Shanghai
})

// Query nearby locations (within 5km)
nearby, err := burinClient.GeoRadius("locations", 
    39.9042, 116.4074, 5.0, "km")
if err != nil {
    panic(err)
}

for _, loc := range nearby {
    fmt.Printf("Location: %s, Distance: %.2f km\n", 
        loc.Member, loc.Distance)
}
```

### Hash Operations

```go
// Set hash fields
err = burinClient.HSet("user:1001", "name", []byte("Alice"))
err = burinClient.HSet("user:1001", "age", []byte("25"))

// Get hash field
value, err := burinClient.HGet("user:1001", "name")

// Get all fields
fields, err := burinClient.HGetAll("user:1001")
for field, value := range fields {
    fmt.Printf("%s: %s\n", field, string(value))
}
```

## Configuration

### Server Configuration Example

```yaml
# Application configuration
app:
  name: "Burin"
  version: "1.0.0"
  environment: "production"
  node_id: "node1"
  data_dir: "./data"
  default_database: "default"

# Log configuration
log:
  level: "info"
  format: "json"
  output: "file"
  file: "./logs/burin.log"

# Cache configuration
cache:
  max_databases: 16
  default_ttl: "1h"
  max_value_size: 1048576  # 1MB
  enable_compression: true

# Consensus configuration
consensus:
  node_id: "node1"
  bind_addr: "127.0.0.1:8001"
  data_dir: "./data/raft"
  bootstrap: true
  join_addresses: []

# Transaction configuration
transaction:
  max_concurrent_transactions: 1000
  transaction_timeout: "30s"
  isolation_level: "read_committed"

# Burin server configuration
burin:
  bind_address: "0.0.0.0:8099"
  max_connections: 1000
  read_timeout: "30s"
  write_timeout: "30s"
  enable_auth: true
```

### Client Configuration

```go
config := client.NewDefaultConfig()

// Connection configuration
config.Connection.Endpoints = []string{
    "node1:8099",
    "node2:8099",
    "node3:8099",
}
config.Connection.DialTimeout = 5 * time.Second
config.Connection.RequestTimeout = 10 * time.Second

// Retry configuration
config.Retry.MaxAttempts = 3
config.Retry.InitialBackoff = 100 * time.Millisecond
config.Retry.MaxBackoff = 5 * time.Second

// Connection pool configuration
config.Pool.MaxSize = 100
config.Pool.MinSize = 10
config.Pool.IdleTimeout = 5 * time.Minute

// Authentication configuration
config.Auth.Username = "burin"
config.Auth.Password = "burin@secret"
```

## Building

### Build All Platform Versions

```bash
# Build server (all platforms)
./build.sh

# Build CLI tool (all platforms)
./build-cli.sh
```

### Build Specific Platform

```bash
# macOS ARM64
GOOS=darwin GOARCH=arm64 go build -o build/burin-darwin-arm64 main.go

# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o build/burin-linux-amd64 main.go
```

### Run Tests

```bash
# Run unit tests
go test ./...

# Run integration tests
cd test_suite
go run main.go
```

## Cluster Deployment

### Docker Deployment (Recommended)

Quickly deploy a three-node cluster using Docker Compose:

```bash
cd docker

# Build image
make build

# Start cluster
make up-d

# Check status
make status
```

For detailed instructions, see the [Docker Deployment Guide](./docker/README.md).

### Local Deployment

Start a three-node cluster:

```bash
# Node 1 (Leader - Bootstrap)
./build/burin-darwin-arm64 -config build/burin-node1.yaml

# Node 2 (Follower)
./build/burin-darwin-arm64 -config build/burin-node2.yaml

# Node 3 (Follower)
./build/burin-darwin-arm64 -config build/burin-node3.yaml
```

Or use the management script:

```bash
# Start all nodes
./start.sh start

# Check status
./start.sh status

# Stop all nodes
./start.sh stop
```

### Node Configuration Key Points

Each node needs to configure:
- Unique `node_id`
- Different `bind_addr` (Raft communication address)
- Different `burin.bind_address` (client service address)
- First node sets `bootstrap: true`
- Other nodes configure `join_addresses` pointing to Leader

## Performance Characteristics

- **High Throughput**: Single node supports tens of thousands of QPS
- **Low Latency**: Average response time < 1ms (local network)
- **Memory Efficient**: Uses BadgerDB LSM tree structure with low memory footprint
- **Concurrency Friendly**: Supports large number of concurrent connections and transactions

## Monitoring and Operations

### Health Check

```bash
# CLI health check
health

# Cluster status
cluster-status
```

### Prometheus Metrics

Burin exposes the following metrics (if enabled):
- Request count and latency
- Cache hit rate
- Transaction success/failure rate
- Connection pool status
- Raft cluster status

## Directory Structure

```
burin/
├── main.go              # Server entry point
├── go.mod               # Go module definition
├── build.sh             # Server build script
├── build-cli.sh         # CLI build script
├── auth/                # Authentication & authorization module
├── business/            # Business logic layer
├── cid/                 # Cluster ID management
├── cli/                 # CLI tool
├── client/              # Go client SDK
├── config/              # Configuration management
├── consensus/           # Raft consensus layer
├── cProtocol/           # Communication protocol
├── examples/            # Usage examples
├── store/               # Storage engine
├── transaction/         # Transaction management
└── test_suite/          # Integration test suite
```

## Contributing

Issues and Pull Requests are welcome!

## License

This project is licensed under the MIT License.

## Related Documentation

- [Client Usage Guide](./client/README.md)
- [Connection Pool Documentation](./client/POOL.md)
- [Transaction Documentation](./client/TRANSACTION.md)
- [Migration Checklist](./client/MIGRATION_CHECKLIST.md)

## Contact

For questions or suggestions, please contact us through Issues.

## Dependencies

The Burin project is built on the following excellent open-source libraries:

### Core Dependencies

| Library | Version | Purpose |
|---------|---------|---------|
| [BadgerDB](https://github.com/dgraph-io/badger) | v4.8.0 | High-performance KV storage engine with LSM tree structure |
| [HashiCorp Raft](https://github.com/hashicorp/raft) | v1.5.0 | Distributed consensus algorithm implementation |
| [raft-boltdb](https://github.com/hashicorp/raft-boltdb) | latest | Raft log storage backend |
| [Logrus](https://github.com/sirupsen/logrus) | v1.9.3 | Structured logging library |

### Utility Libraries

| Library | Version | Purpose |
|---------|---------|---------|
| [Sonic](https://github.com/bytedance/sonic) | v1.14.2 | High-performance JSON serialization/deserialization |
| [Viper](https://github.com/spf13/viper) | v1.21.0 | Configuration file management |
| [Cobra](https://github.com/spf13/cobra) | v1.9.1 | CLI command-line tool framework |
| [Readline](https://github.com/chzyer/readline) | v1.5.1 | Interactive command-line support |
| [Prometheus Client](https://github.com/prometheus/client_golang) | v1.4.0 | Monitoring metrics export |

### Special Thanks

- **BadgerDB**: Provides high-performance, low-latency LSM tree storage engine
- **HashiCorp Raft**: Mature and stable Raft consensus algorithm implementation
- **Sonic**: ByteDance's ultra-high-performance JSON library
- **Viper & Cobra**: Makes configuration management and CLI development simple

---

<p align="center">
  Made with ❤️ by Burin Team
</p>
