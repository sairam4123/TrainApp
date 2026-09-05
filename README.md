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
   - signals (used by interlocking to check path direction, not yet driving real aspects)
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
│   └── kkdiTpjWorld.go  # Newer double-line TPJ-KKDI world with crossovers, signals, and candidate-path generation
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
│   ├── signals.go       # Signal type used by interlocking to check path direction (not yet driving real signal aspects)
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
- A `Signal` marks a point as the boundary a train should normally face correctly to enter a track section; the interlocking (`NavigatesCorrectly`) uses this to prefer correctly-faced candidate paths over wrong-direction ones, but deliberately doesn't reject the wrong-direction ones outright — wrong-way running still needs to be selectable for scheduled line blocks (e.g. single-line working during possessions). Signals do not yet have aspects (proceed/stop/caution) of their own
- A track graph connects points and segments into a searchable network, used to find paths between two points and to check that every segment on that path is reserved for the train using it
- `TrackGraph.GenerateCandidatePaths` enumerates every possible path between a starting point and a target track; the interlocking (see below) sorts these candidates and reserves the first one that succeeds. The older single-path `FindPath` / `FindPathToTrack` are kept for reference but marked `@Deprecated` and are no longer called by the dispatcher
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
This layer makes the system safe. When a train requests a route, the dispatcher asks the interlocking (`Interlocking.TryReservePathTo`) to find a path: it generates every candidate path to the target track, sorts them by whether they navigate in a signal-correct direction and then by distance, and attempts to reserve tracks and lock switches along the best candidate, falling back to the next one if reservation fails. If no candidate can be reserved, the request is queued and retried when a track is released. In practice, this is the layer that turns abstract topology into coordinated, conflict-free train operation.

### 5. Simulation engine
The discrete-event engine drives the system forward. Instead of running a continuous loop, it processes events in time order, such as:

- train arrival
- train departure
- route grant
- route rejection
- movement update
- resource release

This separation allows the system to reason about time explicitly and to update state only when meaningful events occur.

Movement authority (`MovementAuthorized` / `MovementAuthorityEnded`) is defined as an event type in [events.go](railway/events.go), and a `MovementAuthority` type exists in [authority.go](railway/authority.go) to hold and extend a train's authorized path. `Train` now holds its currently-granted authority (`Train.ma`), set whenever `Dispatcher.RequestToProceed` succeeds, but nothing extends it (`MovementAuthority.ExtendByPath` and `NewMovementAuthority` are never called) and the `MovementAuthorityEnded` case in the event loop is still empty.

## main.go: work in progress

[main.go](main.go) contains two versions of the entry point:

- **Active (`func main`)**: calls `worlds.BuildTpjKkdiWorld()`, which now builds a three-station, double-line world — TPJ, an intermediate PDKT, and KKDI, each with four platforms and a crossover — connected by two shorter inter-station legs (TPJ↔PDKT ~55km, PDKT↔KKDI ~41km) instead of one long TPJ↔KKDI segment, with a signal guarding entry at each end of both legs. It creates two trains per iteration (currently scaled down to 2 iterations, i.e. 4 trains) with schedules via `Train.AddSchedule`, adds them to the world, and runs the full discrete-event simulation with `sim.Init()` / `sim.Run()`.
- **Commented out, kept for reference**: an earlier `func main` that calls `worlds.BuildTestWorld()`, the older 3-station (TPJ-PDKT-KKDI) world with block sections instead of signals, and runs a larger set of trains through the same `sim.Init()` / `sim.Run()` flow.

The `worlds/` package holds both sample worlds (`testWorld.go`, `kkdiTpjWorld.go`) so `main.go` can switch between them.

## End-to-end flow

This is the flow the project is built around, and the one the active `main.go` exercises end to end.

1. The railway world is built (see [worlds/](worlds/)) with stations, platforms, tracks, points, signals, and (in the older world) block sections.
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
- Wrong-direction running is a candidate, not a rejection: signals de-prioritize wrong-facing paths rather than ruling them out, since scheduled line blocks (e.g. single-line working during possessions) require a train to legitimately run against the normal direction.

## Known limitations

The current model has a few gaps between what it represents and what it actually enforces:

- **Reservation granularity is coarser than a real block system.** `Interlocking.TryReservePathTo` reserves a path in one shot, up to and including the target platform track, rather than stopping at the next signal. Adding the intermediate PDKT station split the old single 96km TPJ↔KKDI segment into two shorter legs (TPJ↔PDKT ~55km, PDKT↔KKDI ~41km) with a signal at each end, but each leg is still a single `TrackSegment`, so a train holds an entire leg, in one direction, for the whole transit — a following train still can't be granted a route as far as an intermediate signal, because within a leg there isn't one. The `BlockSection` type exists for this kind of subdivision but isn't used by the dispatcher yet, so effective line throughput is well under what the double-line topology should allow.

  This whole-path-up-front behavior also reaches into the *destination* station immediately, not just the departure end: `TryReservePathTo`/`TryReservePath` locks every switch on the path — including the entry switch into the arrival platform — the moment the train departs, long before it physically gets there. A train leaving TPJ locks PDKT's platform-entry switch straight away, well before it reaches PDKT. A second train already sitting at PDKT, ready to leave in the *opposite* direction over that same physical switch (e.g. to clear out toward a different platform), is shut out of the retry queue for that whole transit even though the two trains' actual paths never occupy the same track at the same time — it's an artifact of granting the entire journey's switches up front rather than one signal-bounded leg at a time.
- **Candidate-path search has no pruning.** `TrackGraph.GenerateCandidatePaths` does a full BFS enumeration of every route to the target track, deduplicating only by edge (not by visited point) and with no distance bound or cap. On loop-heavy infrastructure like the crossovers, the number of candidate paths can grow quickly as the network gets bigger, with no timeout to fall back on.
- **No fairness in the reservation retry queue.** `Dispatcher.OnTrackReleased` retries `waitingReservationRequests` in FIFO order whenever a track frees up, but nothing prevents a train that keeps losing a contested resource (e.g. two trains wanting the same single line in opposite directions) from being starved indefinitely. TODO.md already tracks this as future work ("rework reservation requests with priority requests & train priority").
- **The proceed (switch-lock) retry queue is never drained.** When `Dispatcher.RequestToProceed` fails — either `Path.EnsureAllEdgesAreReserved` or `Interlocking.EnsureAllSwitchesLocked` returns false — the request is appended to `Dispatcher.waitingProceedRequests`, but nothing ever reads from that slice again. Unlike `waitingReservationRequests`, which `OnTrackReleased` retries on every track release, a train stuck here just stays stuck; it doesn't even get the FIFO-unfairness treatment above.
- **Movement authority is tracked but inert.** `Train.ma` now holds the currently-granted `MovementAuthority` (set whenever `RequestToProceed` succeeds), but `MovementAuthority.ExtendByPath`/`NewMovementAuthority` are never called and the `MovementAuthorityEnded` case in the event loop (`sim.go`) is still empty — authorities are never extended or ended, they're just replaced wholesale by the next full-path reservation.
- **`PointController` is fully orphaned.** Earlier, `Dispatcher.Init()` built one `PointController` per track point as part of an older locking design; that call is gone now, and nothing anywhere constructs a `PointController` — the type in [points.go](railway/points.go) is unreferenced outside its own file and outside commented-out code in `dispatcher.go`. Switch locking is handled entirely by `SwitchBlock`/`Interlocking` now. Safe to delete once confirmed unneeded.

Two issues from an earlier pass have since been fixed and are no longer limitations: `Interlocking.trackSwitchMap` is now `map[string][]string` (every switch block owning a track ID gets locked/unlocked together, not just the last one registered), and `NewTrackID` normalizes point order, so both directions across a diamond crossing now share the same track ID as the diamond and get jointly locked.

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
