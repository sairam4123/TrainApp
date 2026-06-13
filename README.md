# TrainApp

A railway simulation system written in Go that models train movement, scheduling, and traffic control on railway networks.

## Overview

TrainApp is a discrete event simulation engine for railway systems. It provides a framework to model railway infrastructure, train movements, schedules, and automatic traffic control and dispatching.

## Features

- **Railway World Simulation**: Create and manage railway networks with stations, tracks, and platforms
- **Train Management**: Model trains with schedules, speed profiles, and occupancy tracking
- **Track Control**: Block sections, track points, and signal management
- **Scheduling**: Schedule points and train routing between stations
- **Dispatcher System**: Automatic train coordination and conflict resolution
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
- **TrackGraph**: A graph-based representation of the railway network
- **Train**: Individual train entities with schedules and position tracking
- **Station**: Railway stations with platforms and connections
- **BlockSection**: Safety zones between stations for train separation
- **Dispatcher**: Coordinates train movements and manages scheduling

### Simulation Flow

1. Build the railway world with stations and tracks
2. Add trains with schedules
3. Run the discrete event simulation
4. Process train movements and events
5. Resolve conflicts through the dispatcher

## Development

### Current Work

See [TODO.md](TODO.md) for planned improvements and features in development.

## License

See [LICENSE](LICENSE) file for details.
