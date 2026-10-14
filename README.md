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
├── main.go                  # Entry point: builds the TPJ-PDKT-KKDI world, 12 trains, runs the sim
├── worlds/                  # Sample railway worlds
│   ├── testWorld.go         # Older 3-station (TPJ-PDKT-KKDI) world
│   ├── kkdiTpjWorld.go      # Active double-line TPJ-PDKT-KKDI world with crossovers and signals
│   └── singleLineWorld.go   # Single-line worlds (passing loops, halts, junction) used by the deadlock tests
├── railway/
│   ├── ids/                 # Typed identifiers: StnCode, PointID, TrackID, TrainID
│   ├── infra/               # Static infrastructure (no runtime state)
│   │   ├── world.go         # World: stations, switch blocks, signals, graph; Finalize() builds indexes
│   │   ├── graph.go         # TrackGraph and candidate-path search
│   │   ├── routing.go       # Path ranking: FollowsSignals, IsLegalPath, RankedPaths
│   │   ├── path.go          # Path and Step (an edge with its direction of travel)
│   │   ├── points.go        # TrackPoint
│   │   ├── tracks.go        # TrackSegment
│   │   ├── switch.go        # Switches, diamond crossings and slips (SwitchBlock)
│   │   ├── signals.go       # Signal
│   │   ├── stations.go      # Station and Platform
│   │   └── templates.go     # Helpers to build common station layouts
│   ├── interlocking/        # Runtime safety state: track reservations/occupation, switch setting/locking
│   ├── ops/                 # Train, schedule, MovementAuthority, event types
│   └── sim/                 # Discrete-event loop, TrainController state machine, Dispatcher, deadlock avoidance
├── des/                     # Discrete-event engine and WaitList (resource-based waiting)
├── units/                   # Distance, time and speed helpers
├── visualization/           # Terminal printout of the world's boundary tracks
├── example/                 # Example simulation output
├── go.mod                   # Go module definition
└── README.md                # Project documentation
```

`go run .` writes `logs/logs.log` and a final state dump to `dumps/` in the working directory.

## Architecture

The architecture is organized around a simple idea: model the railway as a set of connected resources, then let trains request access to those resources over time. Packages depend in one direction only: `ids` ← `infra` ← `interlocking` ← `ops` ← `sim`.

### 1. World (`infra`)
`World` is the static railway: stations, track points, track segments, switch blocks and signals. It holds no runtime state; who has reserved a track or locked a switch lives in the interlocking. Call `World.Finalize()` once the world is built: it builds the graph caches, indexes signals and switch blocks by track, and checks the world is consistent (e.g. every signal stands on a real track).

Identifiers are typed (`ids.PointID`, `ids.TrackID`, `ids.StnCode`, `ids.TrainID`), so one kind of id can't be used where another is expected. A `TrackID` is the unordered pair of points the track joins; names like `bsTpjPdkt0` are kept as a display `Name`.

### 2. Network model (`infra`)
- Track points are the nodes of the topology and track segments the edges between them.
- A `SwitchBlock` is a set of edges of which only one can be in use at a time: the two legs of a switch, the two routes over a diamond, the routes through a slip. Creating one also tells the graph which transitions are impossible, e.g. running from one switch leg straight onto the other through the switch's toe.
- A `Signal` stands at a point and faces trains coming from a neighbouring point. `World.FollowsSignals(path)` is true when every signalled track on the path is run the way one of its signals faces; paths that fail it are wrong-direction running. They are de-prioritized rather than ruled out, because scheduled line blocks (e.g. single-line working during possessions) need them. Signals do not yet have aspects of their own.
- `TrackGraph.GenerateCandidatePaths` enumerates the paths from a point to a target track breadth-first. A path never visits a point twice and never makes a forbidden transition, and the search is deterministic and capped at `MaxCandidatePaths`. `World.RankedPaths` orders them signal-correct first, then shortest.
- A `Path` is a start point plus edges. `Path.Steps()` gives each edge with its direction of travel.
- **Signal sections.** `World.EndsAtSignal(step)` is true where a train meets a signal: a real signal facing it, or the invisible signal at each end of every platform. A signal section runs from one such point to the next (`World.SectionEnd`).
- **Reachability.** `TrackGraph.Reach` searches over positions (a point plus the edge the train arrived on), honouring forbidden transitions and an optional per-step filter. `World.CanVisit(pos, stops, blocked)` builds on it: can a train standing at `pos` call at each station in `stops`, in order, without using blocked track? It uses the same direction policy as the dispatcher: the correct way if there is one, otherwise wrong-direction running.

### 3. Interlocking (`interlocking`)
The interlocking owns all runtime safety state and exposes it only through operations:

- `Reserve(path, train)` validates the path, then reserves every track and sets and locks every switch block on it. It is all-or-nothing. On failure it undoes only what that call claimed and returns a `ConflictError` naming the edge and the train in the way. It returns a `Route`.
- `Enter(route)` / `Leave(route)` move the front and rear of the train along the route: a track is occupied when entered, and freed (with its switches unlocked) when left.
- `Release(route)` gives back the part of a route the train has not entered.

A track is never occupied without being reserved by the occupier, and a switch block can only be locked for one of its edges at a time.

### 4. Train model (`ops`)
`Train` is identity, schedule and movement state: the `Route` its front is on and the `MovementAuthority` it runs under. A `MovementAuthority` can only be created from a reserved `Route`, so everything it covers is already held for the train. It never runs past a signal. `NewMovementAuthority` covers the route up to the next signal or platform end ahead of the train, and `Extend` moves it on one signal section at a time.

### 5. Dispatcher and deadlock avoidance (`sim`)
`TrainController` asks the `Dispatcher` for routes:

- `RequestEntry` when a train enters the world: the train appears on a free platform at the sim boundary, preferring the scheduled one.
- `RequestRoute` for onward travel from where the train stands. The dispatcher plans whole paths to the station's platforms, ranked as above: ways to free platforms first (preferred platform first), wrong-direction only if the station can't be reached the right way at all. It then reserves **only the first signal section** of the best one it can get. The train runs up to the next signal and asks again from there. The platform is therefore picked as the train approaches the station, and nothing beyond the next signal is locked for it.

**Deadlock avoidance (station exit check).** Before keeping a reservation, the dispatcher checks the railway would still be in a *safe state* (`safety.go`). A safe state is one where the trains in the world can be ordered so that each, in turn, can call at all its remaining stops using only track that is free or held by trains earlier in the order. This is the banker's algorithm, with routes as the resources and `World.CanVisit` as the "can it finish" test. A reservation that would leave the state unsafe is undone and the request waits. Without special cases, this covers:
- entering trains filling a terminus that arriving trains need;
- a train waiting at a home signal on the only way out of a full station;
- opposing trains meeting on a single line;
- conflicting moves at a junction.

**Waiting on resources.** A request that can't be met waits in a `des.WaitList` on the tracks that stood in its way:
- the conflicting track itself;
- every track of a locked switch (the switch unlocks when its holder leaves any of them);
- for a safety refusal, the tracks held by the trains that couldn't finish.

`OnTrackReleased` wakes only the requests waiting on that track, longest-waiting first.

### 6. Simulation engine (`sim`, `des`)
The discrete-event engine processes events in time order; events at the same time run in the order they were scheduled. `Sim.Run()` pulls each event, logs it, and hands it to the train's `TrainController`. The controller is a small state machine (outside, running, held, dwelling, ready, exited):

`WORLD_ENTER` → `ROUTE_GRANTED` → `MOVEMENT_AUTHORIZED` → `TRACK_ENTER` / `TRACK_TRAVEL_END` per edge (`MOVEMENT_AUTHORITY_EXTENDED` at a signal when the route already continues past it) → `PATH_COMPLETED` → `TRAIN_ARRIVE` at the scheduled station, or `MOVEMENT_AUTHORITY_END` at a signal short of it, which requests the next section → `TRAIN_DWELL_END` → next section, `TRAIN_DEPART` … → `SCHEDULE_END` → `WORLD_EXIT`.

A fatal inconsistency (e.g. an unknown station in a schedule) stops the run and is returned from `Run()`. Trains that never finish are reported as `STUCK` at the end of the log, with the reason they are waiting.

## main.go

[main.go](main.go) builds `worlds.BuildTpjKkdiWorld()`: three stations (TPJ, PDKT, KKDI) with four platforms each, connected by a double line (TPJ↔PDKT ~55km, PDKT↔KKDI ~41km) with crossovers at the termini and a signal at the arrival end of each leg. It adds 12 trains (six each way, 20 minutes apart), runs the simulation, and prints how many trains completed their schedule.

## Key design decisions

- Event-driven simulation: time is advanced through scheduled events rather than a real-time loop.
- Explicit resource reservation: trains must reserve shared track resources before using them, and only the interlocking can change that state.
- Graph-based railway representation, with the layout's impossible moves (switch toe reversals) excluded from path search.
- One-way package dependencies: static infrastructure, safety state, train model and simulation are separate packages.
- Simplified realism: the project focuses on the core ideas of railway operations instead of full operational detail.
- Deterministic behavior: the same scenario produces the same log every run.
- Wrong-direction running is a candidate, not a rejection: it is used only when there is no correct-direction way to a station.

## Known limitations

- **Sections are only as fine as the signalling.** Each inter-station leg of the TPJ-KKDI world is one `TrackSegment` with a signal at the far end, so a following train still waits for the whole leg. Splitting legs into signalled blocks (as `worlds.BuildSingleLineWorld` does) lets trains follow each other closely.
- **The safety check costs more as worlds grow.** It runs a reachability search per train per grant, roughly O(trains² × stops × graph) in the worst case. That is fine for tens of trains. Large worlds will want caching of static reachability, or an incremental check.
- **No priorities.** Waiting requests are served longest-waiting first per resource. There are no train priorities or overtaking decisions (TODO #6).
- **No reversing.** Trains can't change direction, so a schedule that needs a reversal (e.g. EJN → NJN through the junction) can't be run, and the safety check treats it as impossible.

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
