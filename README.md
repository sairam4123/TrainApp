# TrainApp

TrainApp is a Go-based railway simulation. It models how trains move through a railway network, how they follow schedules, and how the system reserves track resources to avoid conflicts.

## Purpose

This project is a simplified railway operations simulator. The goal is not to build a polished graphical app, but to model the core logic behind railway movement in a programmable and testable way. A terminal viewer, `trainviz`, draws a world as a line schematic and replays recorded runs, so you can see what the simulation is doing.

The simulator is meant to answer questions such as:

- How is a railway network represented as data?
- How does a train move through a sequence of connected resources?
- How can the system reserve a path before allowing movement?
- How can conflicting movements be avoided safely?
- How can the system advance through time using discrete events rather than a continuous loop?

## What the project shows

`trainapp` builds a sample railway world and runs a discrete-event simulation. It demonstrates how a railway system can be represented in code and how trains can be scheduled and coordinated safely. `trainviz` draws the world and replays a recorded run.

The emphasis is on modeling the underlying mechanics of railway operation rather than on a polished UI or full operational realism.

## Main concepts

At a high level, the system contains five major parts:

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
   - train priorities: waiting trains are served highest priority first, adjusted for delay
   - precedence: a lower-priority train is held, or moved to another platform, so a higher-priority one can overtake or cross

4. Simulation engine
   - event scheduling
   - time advancement
   - state updates as trains move and arrive
   - coordination between train activity and infrastructure state

5. Visualization
   - a line schematic of any world, drawn in the terminal
   - a recorder that writes a run as a stream of state changes
   - a replay viewer with play/pause, stepping, and per-train following

## Project structure

```text
.
├── cmd/
│   ├── trainapp/            # Runs the simulation: builds the TPJ-PDKT-KKDI world and 12 trains
│   │   └── main.go          # Flags: -log (text|json), -record (write a recording for trainviz), -priorities, -overrides
│   └── trainviz/            # Terminal viewer
│       ├── main.go          # Flags and the two modes: draw a world, or `replay` a recording
│       ├── viewer.go        # Interactive Bubble Tea model: pan, zoom, panes, timeline
│       └── replay.go        # Replays a recording: seeks to a time and rebuilds the state there
├── worlds/                  # Sample railway worlds
│   ├── testWorld.go         # Older 3-station (TPJ-PDKT-KKDI) world
│   ├── kkdiTpjWorld.go      # Active double-line TPJ-PDKT-KKDI world with crossovers and signals
│   └── singleLineWorld.go   # Single-line worlds (passing loops, halts, junction) used by the deadlock tests
├── profiles/                # Per-railway priority and precedence configs (JSON), e.g. ir.json for Indian Railways
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
│   │   ├── station_tracks.go # Station area, main lines, through tracks and loops
│   │   └── templates.go     # Helpers to build common station layouts
│   ├── interlocking/        # Runtime safety state: track reservations/occupation, switch setting/locking
│   ├── ops/                 # Train, schedule (planned and actual times, delay), MovementAuthority, event types
│   └── sim/                 # The run itself:
│       ├── sim.go           #   Sim: owns the world, interlocking and event loop; Run, Init, state dump
│       ├── controller.go    #   TrainController state machine
│       ├── dispatcher.go    #   Dispatcher: entry and route requests, waiting requests
│       ├── safety.go        #   Deadlock avoidance: the safe-state check
│       ├── priority.go      #   Train priority: PriorityConfig (classes, delay bands), SetPriorityAt
│       ├── precedence.go    #   Precedence: conflicts, holds, decisions
│       ├── projection.go    #   Look-ahead: each train's unimpeded future
│       ├── stationplan.go   #   Which station track each train due at a station gets
│       ├── holdrule.go      #   PrecedenceConfig and the hold rules (thresholds, weighted)
│       ├── overrides.go     #   Operator overrides of precedence decisions
│       ├── log.go           #   Structured run log (logfmt or JSON lines), STUCK report
│       └── observer.go      #   Observer hook that lets the recorder watch a run
├── des/                     # Discrete-event engine and WaitList (resource-based waiting)
├── units/                   # Distance, time and speed helpers
├── visualization/
│   ├── vizproto/            # The stream trainapp writes and trainviz reads (newline-delimited JSON)
│   ├── record/              # Records a sim run as that stream (a sim.Observer)
│   ├── schematic/           # Lays a world out as a line schematic and draws it in the terminal
│   └── viz_term.go          # Older plain-text printout of the track graph; no command uses it
├── example/                 # Example simulation output (logs.txt, from an earlier log format)
├── TODO.md                  # Planned work
├── LICENSE
├── go.mod                   # Go module definition
└── README.md                # Project documentation
```

Tests sit next to the code they cover (`*_test.go`); see [Run the tests](#run-the-tests).

### Output files

`go run ./cmd/trainapp` writes its output under the working directory:

- `logs/` holds the run log.
- `dumps/` holds the final state dump, `dump<N>.txt`: each train's phase, and who has reserved or occupies every track. Dumps from earlier runs are cleared when a run starts.
- With `-record <file>`, the recording goes to the path you give.

Git ignores `*.log` and `dumps/`.

The log is structured, one entry per line, with the same keys in either format (see `railway/sim/log.go` for the full list):

- **Text (the default)** is logfmt, written to `logs/logs.log`:

  ```text
  t=32 level=INFO event=ROUTE_WAIT train=0452U name=Train2Up station=PDKT pf=1 entry=false reason="0452U cannot use bsTpjPdkt0: track reserved by 0451U" blocked_by=bsTpjPdkt0 priority=20 delay=0.01
  ```
- **JSON lines** are written to `logs/logs.jsonl`, with `go run ./cmd/trainapp -log json`.

Each entry starts with `t` (sim minutes), `level` and `event`. Times come from the simulation clock, so the same scenario always produces the same log.

## Architecture

The architecture is organized around a simple idea: model the railway as a set of connected resources, then let trains request access to those resources over time. Packages depend in one direction only: `ids` ← `infra` ← `interlocking` ← `ops` ← `sim`. The visualization packages sit on top: `vizproto` knows only the world (`infra`), `schematic` draws from `vizproto`, and only `record` knows about `sim`.

### 1. World (`infra`)
`World` is the static railway: stations, track points, track segments, switch blocks and signals. It holds no runtime state; who has reserved a track or locked a switch lives in the interlocking. Call `World.Finalize()` once the world is built: it builds the graph caches, indexes signals and switch blocks by track, and checks the world is consistent (e.g. every signal stands on a real track).

Identifiers are typed (`ids.PointID`, `ids.TrackID`, `ids.StnCode`, `ids.TrainID`), so one kind of id can't be used where another is expected. A `TrackID` is the unordered pair of points the track joins; names like `bsTpjPdkt0` are kept as a display `Name`.

### 2. Network model (`infra`)
- Track points are the nodes of the topology and track segments the edges between them.
- Edges run west to east: `AddTrack(from, to, ...)` takes the west end first, and adding a track again the other way round panics. This only fixes how the world is drawn; trains run either way. Templates also record the drawing: `NewSwitch` takes a `SwitchLayout` (which way the legs leave the toe, and how many lanes the diverging leg moves), and `NewDiamondCrossing` and the slips take how many lanes the crossing tracks move. A plain track can be moved off its lane with `GraphEdge.SetLanes`.
- A `SwitchBlock` is a set of edges of which only one can be in use at a time: the two legs of a switch, the two routes over a diamond, the routes through a slip. Creating one also tells the graph which transitions are impossible, e.g. running from one switch leg straight onto the other through the switch's toe.
- A `Signal` stands at a point and faces trains coming from a neighbouring point. `World.FollowsSignals(path)` is true when every signalled track on the path is run the way one of its signals faces; paths that fail it are wrong-direction running. They are de-prioritized rather than ruled out, because scheduled line blocks (e.g. single-line working during possessions) need them. Signals do not yet have aspects of their own.
- `TrackGraph.GenerateCandidatePaths` enumerates the paths from a point to a target track breadth-first. A path never visits a point twice and never makes a forbidden transition, and the search is deterministic and capped at `MaxCandidatePaths`. `World.RankedPaths` orders them signal-correct first, then shortest.
- A `Path` is a start point plus edges. `Path.Steps()` gives each edge with its direction of travel.
- **Signal sections.** `World.EndsAtSignal(step)` is true where a train meets a signal: a real signal facing it, or the invisible signal at each end of every platform. A signal section runs from one such point to the next (`World.SectionEnd`).
- **Station tracks.** A station's area is the tracks whose two end points carry its code. For each running line that enters it, the **main line** is the platform track the line connects to directly: the one reached over the way whose slowest track is fastest (in practice, straight switch legs only). In the TPJ-KKDI world that is pf2 for the up line and pf3 for the down line at all three stations. A platform track with platform length 0 is a **through track**: it can be timetabled, but nobody boards there. Every other platform track is a **loop**. `Station.TrackClass`, `MainFor` and `MainRoute` expose this; `Station.RunsThrough` is false for a terminus.
- **Reachability.** `TrackGraph.Reach` searches over positions (a point plus the edge the train arrived on), honouring forbidden transitions and an optional per-step filter. `World.CanVisit(pos, stops, blocked)` builds on it: can a train standing at `pos` call at each station in `stops`, in order, without using blocked track? It uses the same direction policy as the dispatcher: the correct way if there is one, otherwise wrong-direction running.

### 3. Interlocking (`interlocking`)
The interlocking owns all runtime safety state and exposes it only through operations:

- `Reserve(path, train)` validates the path, then reserves every track and sets and locks every switch block on it. It is all-or-nothing. On failure it undoes only what that call claimed and returns a `ConflictError` naming the edge and the train in the way. It returns a `Route`.
- `Enter(route)` / `Leave(route)` move the front and rear of the train along the route: a track is occupied when entered, and freed (with its switches unlocked) when left.
- `Release(route)` gives back the part of a route the train has not entered.

A track is never occupied without being reserved by the occupier, and a switch block can only be locked for one of its edges at a time.

### 4. Train model (`ops`)
`Train` is identity, schedule and movement state: the `Route` its front is on and the `MovementAuthority` it runs under. Each `SchedulePoint` keeps the actual arrival and departure next to the timetabled ones, and `Train.Delay(now)` derives from them how late the train is running (growing while it waits past a timetabled time). A `MovementAuthority` can only be created from a reserved `Route`, so everything it covers is already held for the train. It never runs past a signal. `NewMovementAuthority` covers the route up to the next signal or platform end ahead of the train, and `Extend` moves it on one signal section at a time.

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

`OnTrackReleased` wakes only the requests waiting on that track, highest priority first, then longest-waiting.

**Schedules name every station.** `Sim.Init` rejects a train that would run through a station without a schedule point for it; a station a train does not stop at gets a pass point (`ops.NewPassPoint`, untimed with a negative time).

**Priority.** A train's priority is a number, higher served first: its base `Priority`, adjusted by the delay band it falls in. Both come from a `sim.PriorityConfig` (`Sim.Priorities`), loaded from JSON: `classes` maps a train's `Class` to its base priority, and `delay_bands` adjust it by lateness (the band with the largest `over_min` below the delay applies). The sim gives classes and thresholds no meaning of their own; `profiles/ir.json` holds Indian Railways' (VB Express highest down to Special Express; up to 15 min late is boosted as recoverable, over an hour is lowered as unrecoverable). Priority is worked out when it is needed, so it follows the delay as it grows, and `Sim.SetPriorityAt` changes a train's base priority during a run (logged as `PRIORITY_CHANGE`).

**Precedence** (on when the config has a `precedence` block; see `docs/trainapp/precedence/plan.md` for the design). Before granting a train L anything, the dispatcher looks ahead (`projection.go`): each train's unimpeded future, at the track speed limits, and for a train already waiting on another's track, from when that track is due to be free. It then asks whether L going now would cost a higher-priority train H time, in one of three ways:
- **overtake**: H would catch up with L, both running the same way;
- **cross**: they would meet head on on single line, or at a switch or diamond;
- **platform**: L would take the station track H needs.

If so, and the hold rule agrees, L is **held** where it stands until H is past, as long as it stands somewhere H can get by it (a refuge). The rule is `thresholds` (priority gap and minutes saved) or `weighted` (priority × minutes, saved against waited), picked by name, and `max_hold_min` caps how long a train is held at one place in all. At stations the **station plan** may move L to another track instead. Station tracks are given out highest priority first: through tracks are kept for trains running through, a stopping train may use the main line only for a stay up to `main_line_max_stay_min` (a terminus has no limit, since nothing runs through it), a train running through prefers a through track, then its main line, and between equal choices a train keeps to its own side of the station rather than crossing the other line. Platform moves are logged as `PLATFORM_CHANGE`, holds as `PRECEDENCE_HOLD` and `PRECEDENCE_RELEASE`, and a hold the rule wanted but could not place as `PRECEDENCE_SKIPPED` (with why: `no_refuge`, `unsafe`, `max_hold`, `main_line_limit`, `through_track`, `cycle`).

A hold must not fight deadlock avoidance: the safety check knows about every active hold (the held train can only finish after the other has reached its milestone, the first station past it), so no other grant can strand H behind L, and a hold that would make the state unsafe is never placed. With precedence on, the check also judges each train where it will stand once its granted route is run, and may move a train on a stop at a time when none can finish outright; both keep a move judged safe during a hold from looking unsafe once the hold ends. With precedence off the check is unchanged.

**Overrides.** Every decision has an id that stays the same from run to run (`hold:<train>><for>@<place>`, `pf:<train>@<station>`). `Sim.Override` / `Sim.OverrideAt` and `trainapp -overrides file.json` release (or veto) a hold, force one, pin a train's platform, or turn precedence off for a train:

```json
{"overrides": [
  {"t": 40, "action": "release", "decision": "hold:0452U>0451U@PDKT"},
  {"action": "hold", "train": "0454U", "for": "0453U", "station": "PDKT"},
  {"action": "platform", "train": "0453D", "station": "PDKT", "pf": "4"},
  {"action": "disable", "train": "0455U"}
]}
```

### 6. Simulation engine (`sim`, `des`)
The discrete-event engine processes events in time order; events at the same time run in the order they were scheduled. `Sim.Run()` pulls each event, logs it, and hands it to the train's `TrainController`. The controller is a small state machine (outside, running, held, dwelling, ready, exited):

`WORLD_ENTER` → `ROUTE_GRANTED` → `MOVEMENT_AUTHORIZED` → `TRACK_ENTER` / `TRACK_TRAVEL_END` per edge (`MOVEMENT_AUTHORITY_EXTENDED` at a signal when the route already continues past it) → `PATH_COMPLETED` → `TRAIN_ARRIVE` at the scheduled station, or `MOVEMENT_AUTHORITY_END` at a signal short of it, which requests the next section → `TRAIN_DWELL_END` → next section, `TRAIN_DEPART` … → `SCHEDULE_END` → `WORLD_EXIT`.

A fatal inconsistency (e.g. an unknown station in a schedule) stops the run and is returned from `Run()`. Trains that never finish are logged as `STUCK` entries (`level=WARN`) at the end of the log, with the reason they are waiting, and a failed run ends with a `SIM_ERROR` entry.

### 7. Visualization (`visualization`, `cmd/trainviz`)
The simulation never knows it is being drawn. A `sim.Observer` can be set on the `Sim`; it is handed every log entry, and told after each event once the train's controller has run.

- **`vizproto`** is the stream between the two programs: newline-delimited JSON, one message per line, each with a type and the sim time. A `hello` describes the world, then `track`, `switch`, `train` and `event` messages report what changed, and the stream ends with `end` (or `error` if the run failed). It reports state changes rather than sim events, so the viewer does not depend on how the sim works inside.
- **`record`** is the `Observer` that writes that stream. After every event it compares the interlocking and the trains with what it last sent, and sends only the difference. Recording does not change the run or its log.
- **`schematic`** lays a world out as a line schematic: tracks run west to east along horizontal lanes, and switches and crossovers move between lanes. It uses the lane information the templates recorded (see Network model). Nothing is drawn to scale. It renders at three levels (overview, layout, detail) and can overlay a replayed state.
- **`trainviz`** is the front end: it draws a world, or opens a recording with `replay`, using [Bubble Tea](https://github.com/charmbracelet/bubbletea). See [Draw a world](#draw-a-world) and [Replay a run](#replay-a-run).

## cmd/trainapp

[cmd/trainapp/main.go](cmd/trainapp/main.go) builds `worlds.BuildTpjKkdiWorld()`: three stations (TPJ, PDKT, KKDI) with four platforms each, connected by a double line (TPJ↔PDKT ~55km, PDKT↔KKDI ~41km) with crossovers at the termini and a signal at the arrival end of each leg. It adds 12 trains (six each way, 20 minutes apart), runs the simulation, and prints how many trains completed their schedule, e.g. `12/12 trains completed their schedule by t=234.27 (log: logs/logs.log)`.

| Flag | Default | Effect |
|---|---|---|
| `-log` | `text` | Log format: `text` (logfmt, `logs/logs.log`) or `json` (`logs/logs.jsonl`) |
| `-record` | none | Also write the run to this file, for `trainviz replay` |
| `-priorities` | `profiles/ir.json` | Priority config (classes, delay bands and precedence); empty for none, which leaves every train at priority 0 and precedence off |
| `-overrides` | none | Precedence overrides to apply (see [Precedence](#5-dispatcher-and-deadlock-avoidance-sim)) |

The other worlds in `worlds/` are not run by `trainapp`. The single-line and junction worlds are used by the deadlock tests, and all of them can be drawn with `trainviz`.

## Key design decisions

- Event-driven simulation: time is advanced through scheduled events rather than a real-time loop.
- Explicit resource reservation: trains must reserve shared track resources before using them, and only the interlocking can change that state.
- Graph-based railway representation, with the layout's impossible moves (switch toe reversals) excluded from path search.
- One-way package dependencies: static infrastructure, safety state, train model and simulation are separate packages.
- Simplified realism: the project focuses on the core ideas of railway operations instead of full operational detail.
- Deterministic behavior: the same scenario produces the same log every run.
- Trains always obey their movement authority, so signals passed at danger (SPADs) and other failures are not modelled: they would make a run probabilistic. If they are added later, they will need braking physics (TODO #7) and a seeded random source, so that the same seed still gives the same log.
- Wrong-direction running is a candidate, not a rejection: it is used only when there is no correct-direction way to a station.

## Known limitations

- **Sections are only as fine as the signalling.** Each inter-station leg of the TPJ-KKDI world is one `TrackSegment` with a signal at the far end, so a following train still waits for the whole leg. Splitting legs into signalled blocks (as `worlds.BuildSingleLineWorld` does) lets trains follow each other closely.
- **The safety check costs more as worlds grow.** It runs a reachability search per train per grant, roughly O(trains² × stops × graph) in the worst case. That is fine for tens of trains. Large worlds will want caching of static reachability, or an incremental check.
- **Precedence looks ahead, but simply.** Projections assume each train runs free once it can set off, so a hold can last longer than needed; `max_hold_min` and review events bound it. No train takes refuge on a main line or through track yet. Each grant costs a projection and a few reachability searches per higher-priority train, on top of the safety check (TODO 18).
- **No reversing.** Trains can't change direction, so a schedule that needs a reversal (e.g. EJN → NJN through the junction) can't be run, and the safety check treats it as impossible.

## Getting started

### Requirements

- Go 1.26.3 or newer

Dependencies (Bubble Tea, Lip Gloss and a few helpers for the viewer) are fetched by Go on first build.

### Run the simulation

From the project root, run:

```bash
go run ./cmd/trainapp
go run ./cmd/trainapp -log json            # JSON-lines log instead of logfmt
go run ./cmd/trainapp -record run.ndjson   # also record the run for trainviz
```

This prints how many trains completed their schedule, and writes the log and the final state dump (see [Output files](#output-files)).

### Draw a world

```bash
go run ./cmd/trainviz -world tpjkkdi         # full screen: pan and zoom
go run ./cmd/trainviz -print                 # print once
go run ./cmd/trainviz -print -zoom 2         # print once, wider
```

`-world` is one of `tpjkkdi`, `test`, `single` or `junction`. With `-print`, `-zoom 1` is the most compact, `-level` is `overview` (stations collapsed to boxes), `layout` or `detail` (with point, track and switch ids), and `-color` is `auto`, `always` or `never`. The schematic is not to scale: lines between stations are labelled with their length instead.

In the viewer, `+`/`-` or the mouse wheel zoom, `hjkl`, the arrows or a mouse drag pan, `/` then a station code jumps to it, `0` fits the world on screen, and `q` quits.

```text
  TPJ                    PDKT                   KKDI
[═Pf1═══               ══Pf1═══               ══Pf1══]
        ╲             ╱        ╲             ╱
[═Pf2═══─────(55km)────══Pf2═══───(41km)──────══Pf2══]
          ╳                                ╳
[═Pf3═══─────(55km)────══Pf3═══───(41km)──────══Pf3══]
        ╱             ╲        ╱             ╲
[═Pf4═══               ══Pf4═══               ══Pf4══]
```

### Replay a run

```bash
go run ./cmd/trainapp -record run.ndjson
go run ./cmd/trainviz replay run.ndjson
go run ./cmd/trainviz replay -print -at 34.5 run.ndjson   # one frame, as text
```

The recording is newline-delimited JSON (`visualization/vizproto`): a `hello` with the world, then after each sim event the log entries and every track, switch and train that changed. Recording does not change the run or its log. With `-print`, `-at` picks the sim time to show (the end of the run by default), and the frame is followed by a list of the trains and what each waits for.

Tracks are grey when free, green when reserved and red when occupied. A switch leg that is not set is drawn faint. Trains are labelled at their front, e.g. `0451U▶`. Signals (`▶ ◀`) show green when the track beyond is reserved for the approaching train, and red otherwise. The side pane lists each train's phase and what it waits for, and the events pane shows the latest log entries.

Precedence shows too: a train held for another is drawn in amber with `⏸` (`0452U▶⏸`), and the side pane says why (`held at PDKT for 0451U (overtake, +6.2m)`, `pf 3→4 at KKDI (main_line_stay)`). `d` swaps the events pane for the decisions, active ones first; `enter` there jumps to the next decision and selects its train. Main lines have their platform label underlined (and say `(main)` at the detail level), and a through track is labelled `T1` rather than `Pf1`. A replay cannot change a run: use `-overrides` and run again.

| Key | Action |
|---|---|
| `space` | play / pause |
| `[` `]` | slower / faster (x1, x10, x60, x600 sim minutes per real minute) |
| `.` `,` | step one event forward / back |
| `tab`, `shift+tab`, `esc` | select a train / clear the selection |
| `shift+←` `shift+→` | previous / next event of the selected train |
| `f` | follow the selected train |
| `home` `end` | jump to the start / end |
| `d`, `enter` | show the precedence decisions; jump to the next one |

The pan, zoom, `/` and `q` keys work in the replay too. Clicking on the timeline seeks to that time.

### Run the tests

```bash
go test ./...
```

The deadlock tests (`railway/sim`) run trains over the single-line and junction worlds, and the other packages test their own pieces (graph search, interlocking, movement authority, the event engine, the recorder, the schematic and the viewer).

### Build the programs

```bash
go build -o trainapp ./cmd/trainapp
go build -o trainviz ./cmd/trainviz
```

## Development

See [TODO.md](TODO.md) for planned work and improvements.

## License

See [LICENSE](LICENSE) for the license text.
