# TrainApp

TrainApp is a Go-based railway simulation. It models how trains move through a railway network, how they follow schedules, and how the system reserves track resources to avoid conflicts.

## Purpose

This project is a simplified railway operations simulator. The goal is not to build a graphical app, but to model the core logic behind railway movement in a programmable and testable way.

The simulator is meant to answer questions such as:

- How is a railway network represented as data?
- How does a train move through a sequence of connected resources?
- How can the system reserve a path before allowing movement?
- How can conflicting movements be avoided safely?
- How can the system advance through time using discrete events rather than a continuous loop?

## What the project shows

The program builds a sample railway world and runs a discrete-event simulation. It demonstrates how a railway system can be represented in code and how trains can be scheduled and coordinated safely.

The emphasis is on modeling the underlying mechanics of railway operation rather than on a polished UI or full operational realism.

## Main concepts

At a high level, the system contains four major parts:

1. Infrastructure
   - stations and platforms
   - track segments
   - points and switches
   - block sections (modeled, not yet enforced by the dispatcher)
   - route topology and connectivity

2. Trains
   - train identity and schedule
   - current location and movement state
   - route requests and progress
   - arrival and departure behavior at stations

3. Control logic
   - dispatcher decisions
   - interlocking and safety checks
   - route allocation for shared resources
   - queueing and retry behavior when a path is blocked

4. Simulation engine
   - event scheduling
   - time advancement
   - state updates as trains move and arrive
   - coordination between train activity and infrastructure state

## Project structure

```text
.
├── main.go              # Entry point (work in progress, see below)
├── worlds/              # Sample railway worlds used by main.go
│   ├── testWorld.go     # Older 3-station world (TPJ-PDKT-KKDI) with block sections and trains
│   └── tpjKkdiWorld.go  # Newer TPJ-KKDI world with crossovers and candidate-path generation
├── railway/             # Core simulation code
│   ├── world.go         # Main container for stations, tracks, trains, and state
│   ├── train.go         # Train model, schedule, and movement logic
│   ├── stations.go      # Station and platform definitions
│   ├── tracks.go        # Track segments and route pieces
│   ├── points.go        # Track points and node behavior
│   ├── switch.go        # Switches, diamond crossings, and slips
│   ├── blocks.go        # Block sections and occupancy control
│   ├── graph.go         # Track graph and pathfinding over the network
│   ├── path.go          # Path representation and reservation checks for a train's route
│   ├── authority.go     # MovementAuthority type for granting/extending a train's authorized path
│   ├── signals.go       # Signal type definition (not yet wired into the simulation)
│   ├── templates.go     # Helpers to build common station layouts
│   ├── dispatcher.go    # Route granting and conflict handling
│   ├── interlocking.go  # Safety rules for shared resources
│   ├── sim.go           # Simulation orchestration
│   └── events.go        # Event definitions and handlers
├── des/                 # Discrete-event engine
├── units/               # Distance and speed helpers
├── example/             # Example simulation output
├── dumps/               # Example output files
├── logs/                # Runtime logs
├── go.mod               # Go module definition
└── README.md            # Project documentation
```

## Architecture

The architecture is organized around a simple idea: model the railway as a set of connected resources, then let trains request access to those resources over time.

### 1. World
The World object is the top-level container. It holds the full railway model, including stations, tracks, points, block sections, and trains. It is the shared context in which all other components operate.

### 2. Network model
The network is built from connected railway elements:

- Track points act as nodes in the topology
- Track segments connect those nodes and represent physical travel sections
- Switches change route choices at junctions and influence path selection
- Block sections are intended to protect track sections so trains do not occupy the same space at the same time, though this is not yet enforced by the dispatcher
- A `Signal` type exists to represent signals at track points, but it is not yet instantiated or used by the simulation
- A track graph connects points and segments into a searchable network, used to find a path between two points and to check that every segment on that path is reserved for the train using it
- `TrackGraph.GenerateCandidatePaths` is an in-progress replacement for pathfinding that enumerates every possible path between two tracks instead of just one; the older `FindPath` / `FindPathToTrack` are kept for reference but marked `@Deprecated`
- A `Path` can be extended track-by-track as a train's route grows

This layer is responsible for representing the physical form of the railway and the possible movement relationships between elements.

### 3. Train model
Each train has a schedule and a lifecycle that can be understood as a sequence of state transitions. It can:

- enter the network
- request a route
- reserve required resources
- move along track sections
- arrive at or depart from stations
- wait when a route is unavailable
- continue to the next leg of its journey

This layer captures the train as an active participant in the system rather than as a passive record.

### 4. Control layer
This layer makes the system safe. The dispatcher decides whether a train can proceed, and interlocking logic prevents conflicting access to shared resources. In practice, this is the layer that turns abstract topology into coordinated train operation.

### 5. Simulation engine
The discrete-event engine drives the system forward. Instead of running a continuous loop, it processes events in time order, such as:

- train arrival
- train departure
- route grant
- route rejection
- movement update
- resource release

This separation allows the system to reason about time explicitly and to update state only when meaningful events occur.

Movement authority (`MovementAuthorized` / `MovementAuthorityEnded`) is defined as an event type in [events.go](railway/events.go), and a corresponding `MovementAuthority` type now exists in [authority.go](railway/authority.go) to hold and extend a train's authorized path. Neither is yet triggered or handled by the dispatcher or event engine.

## main.go: work in progress

[main.go](main.go) is being reworked and currently contains two versions of the entry point:

- **Active (`func main`)**: calls `worlds.BuildTpjKkdiWorld()`, which builds a two-station world (TPJ and KKDI, each with four platforms and a crossover) and does some basic ad-hoc testing of `TrackGraph.GenerateCandidatePaths`. No trains are scheduled and the discrete-event simulation is not started.
- **Commented out, kept for reference**: an earlier `func main` that calls `worlds.BuildTestWorld()`, creates trains with schedules via `Train.AddSchedule`, adds them to the world, and runs the full discrete-event simulation with `sim.Init()` / `sim.Run()`. This reflects the older, more complete end-to-end flow described below and will be reconciled with the candidate-path work above.

The `worlds/` package holds both sample worlds (`testWorld.go`, `tpjKkdiWorld.go`) so `main.go` can switch between them while this is in progress.

## End-to-end flow

This is the flow the project is built around, and the one exercised by the commented-out simulation path in `main.go` (see above); the currently active `main.go` only builds a world and lists candidate paths, and does not yet run this flow end to end.

1. The railway world is built (see [worlds/](worlds/)) with stations, platforms, tracks, points, and block sections.
2. Trains are created and assigned schedules that define when they should arrive at or depart from stations.
3. A train requests a route when it wants to move.
4. The dispatcher checks whether the required resources are free and whether the route is safe.
5. If the route is accepted, the train occupies the relevant sections and continues moving.
6. As each train progresses, the event engine updates the state of the world and triggers the next relevant event.
7. When a route cannot be granted, the system waits for the blocking resource to be released or for the train to be re-evaluated later.
8. The process repeats until the trains complete their planned journeys or the simulation reaches its end condition.

## Key design decisions

The main design choices are:

- Event-driven simulation: time is advanced through scheduled events rather than a real-time loop.
- Explicit resource reservation: trains must reserve shared track resources before using them.
- Graph-based railway representation: the network is modeled as connected nodes and edges.
- Separation of responsibilities: infrastructure, train logic, control logic, and the engine are kept distinct.
- Simplified realism: the project focuses on the core ideas of railway operations instead of full operational detail.
- Deterministic behavior: the flow of events is organized so the model can be reasoned about and extended predictably.

## Getting started

### Requirements

- Go 1.26.3 or newer

### Run the simulation

From the project root, run:

```bash
go run .
```

### Build the program

```bash
go build -o trainapp
```

## Development

See [TODO.md](TODO.md) for planned work and improvements.

## License

See [LICENSE](LICENSE) for the license text.
