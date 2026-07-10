# TrainApp

A railway simulation system written in Go that models train movement, scheduling, and traffic control on railway networks.

## Overview

TrainApp is a discrete event simulation engine for railway systems. It provides a framework to model railway infrastructure, train movements, schedules, and automatic traffic control and dispatching.

## Features

- **Railway World Simulation**: Create and manage railway networks with stations, tracks, and platforms
- **Train Management**: Model trains with schedules, speed profiles, and occupancy tracking
- **Track Graph & Pathfinding**: Graph-based track network with path finding between stations/platforms
- **Point Locking & Route Reservation**: Dispatcher reserves paths, sets switch states, and locks track points ahead of a train to prevent conflicting routes
- **Block Sections**: Safety zones between stations for train separation
- **Dispatcher System**: Automatic train coordination, route granting, and queuing of waiting reservation/proceed requests
- **Unit Conversion**: Built-in support for distance and velocity measurements

## Project Structure

```
.
├── main.go              # Entry point and world setup
├── railway/             # Core simulation engine
│   ├── world.go         # Main world/simulation container
│   ├── train.go         # Train model and scheduling
│   ├── stations.go      # Station definitions
│   ├── tracks.go        # Track segments and management
│   ├── points.go        # Track points (nodes in the network)
│   ├── blocks.go        # Block sections for train safety
│   ├── graph.go         # Graph representation of track network
│   ├── path.go          # Path finding between stations
│   ├── dispatcher.go    # Train dispatcher/controller
│   ├── events.go        # Simulation events
│   └── sim.go           # Simulation engine
├── units/               # Unit conversion utilities
│   └── measure.go       # Distance and velocity measurements
├── des/                 # Discrete event simulation framework
│   └── des.go           # Event scheduling and processing
├── go.mod              # Go module definition
└── README.md           # This file
```

## Getting Started

### Prerequisites

- Go 1.26.3 or later

### Building

```bash
go build -o trainapp
```

### Running

```bash
go run main.go
```

## Architecture

### Core Components

- **World**: The main simulation container that manages all railway entities (trains, stations, tracks)
- **TrackGraph**: A graph-based representation of the railway network, made up of `TrackPoint` nodes and `TrackSegment`/`GraphEdge` links
- **Train**: Individual train entities with schedules, occupation, and reservation state
- **Station**: Railway stations with platforms and connections
- **BlockSection**: Safety zones between stations for train separation
- **Dispatcher**: Reserves paths for trains, coordinates `PointController`s to lock/unlock track points and set switch states, and queues requests that can't be granted immediately
- **Sim**: Drives the discrete event simulation loop (`des` package) and reacts to train/track lifecycle events (e.g. `WorldEntered`, `TrackEntered`, `RouteGranted`, `TrainArrived`, `TrainDeparted`)

### Simulation Flow

1. Build the railway world with stations, track points, and tracks
2. Add trains with schedules and build the track graph's cache map
3. On simulation start, each train requests a path reservation to its first scheduled platform
4. The dispatcher reserves track segments, sets switch states, and locks the points along the path
5. As the train moves, it acquires/releases track segments and re-requests reservations for the next leg of its journey
6. Reservation or proceed requests that fail (due to conflicts) are queued and retried once the blocking track/point is released

## Development

### Current Work

See [TODO.md](TODO.md) for planned improvements and features in development.

## License

See [LICENSE](LICENSE) file for details.
