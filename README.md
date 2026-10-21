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
│   ├── trainapp/            # Runs the simulation: builds the TPJ-PDKT-KKDI world and the demo trains, prints the KPIs
│   │   └── main.go          # Flags: -world, -schedule, -demo, -disruptions, -save-world, -save-schedule, -log (text|json), -record (write a recording for trainviz), -priorities, -overrides, -timetable, -check-record, -kpi
│   ├── trainbench/          # Runs the benchmark scenarios under several precedence rules and prints a KPI table
│   ├── traindemo/           # Scenario picker with world and schedule previews; runs trainapp, then trainviz; opens trainedit
│   ├── trainedit/           # Scenario editor: the world on its schematic, the trains, the scenario; runs and exports timetables
│   └── trainviz/            # Terminal viewer
│       ├── main.go          # Flags and the two modes: draw a world, or `replay` a recording
│       ├── viewer.go        # Interactive Bubble Tea model: pan, zoom, panes, timeline
│       └── replay.go        # Replays a recording: seeks to a time and rebuilds the state there
├── worlds/                  # Sample railway worlds; load.go loads one by name or from a snapshot
│   ├── testWorld.go         # Older 3-station (TPJ-PDKT-KKDI) world
│   ├── kkdiTpjWorld.go      # Active double-line TPJ-PDKT-KKDI world with crossovers and signals
│   ├── tpjKkdiSingleLineWorld.go # The real TPJ-KKDI single line: all 10 stations, loops and sidings, from OpenStreetMap
│   ├── singleLineWorld.go   # Single-line worlds (passing loops, halts, junction) used by the deadlock tests
│   ├── templates/           # Station (on one line or several), throat, crossover, halt, crossing station and block section builders, reusable
│   └── edit/                # Edits on a world snapshot for trainedit: extend, split, switch, crossover, lines, junction, diamond and slip, signal, stamp, ...; undo; problems
├── profiles/                # Per-railway priority, precedence, timetable and KPI settings (JSON), e.g. ir.json for Indian Railways
├── scenarios/               # Scenarios (world, schedule or timetable, profile, disruptions); schedules/ holds their trains
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
│   │   ├── snapshot.go      # World snapshots: a world as plain data, saved to and loaded from JSON
│   │   ├── stations.go      # Station and Platform
│   │   ├── station_tracks.go # Station area, main lines, through tracks and loops
│   │   └── templates.go     # Helpers to build common station layouts
│   ├── interlocking/        # Runtime safety state: track reservations/occupation, switch setting/locking
│   ├── ops/                 # Train, schedule (planned and actual times, delay), schedule files (JSON), disruptions, MovementAuthority, event types
│   ├── timetable/           # Turns a finished run into a timetable: halts and why, rounding, the check run
│   ├── kpi/                 # Measures a finished run: delay against free running, punctuality, waits, track occupancy
│   ├── scenario/            # Scenario files: world, schedule, timetable, profile, disruptions; the rule to run under
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
│   ├── schematic/           # Lays a world out as a line schematic and draws it in the terminal; the corridor (distance axis)
│   ├── traingraph/          # Draws a recorded run as a time-distance graph
│   ├── runs/                # Runs scenarios with trainapp for traindemo and trainedit, and reads back what a run writes
│   ├── tui/                 # What the terminal apps draw alike: boxes, styles, the class palette, a pan and zoom camera
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
- With `-timetable <file>`, the timetable goes to the path you give, and the check run writes its own log and dump under `logs/check/`.

Git ignores `*.log` and `dumps/`.

The log is structured, one entry per line, with the same keys in either format (see `railway/sim/log.go` for the full list):

- **Text (the default)** is logfmt, written to `logs/logs.log`:

  ```text
  t=32 level=INFO event=ROUTE_WAIT train=0452U name=Train2Up station=PDKT pf=1 entry=false reason="0452U cannot use bsTpjPdkt0: track reserved by 0451U" blocked_by=bsTpjPdkt0 priority=20 delay=0.01
  ```
- **JSON lines** are written to `logs/logs.jsonl`, with `go run ./cmd/trainapp -log json`.

Each entry starts with `t` (sim minutes), `level` and `event`. Times come from the simulation clock, so the same scenario always produces the same log.

## Architecture

The architecture is organized around a simple idea: model the railway as a set of connected resources, then let trains request access to those resources over time. Packages depend in one direction only: `ids` ← `infra` ← `interlocking` ← `ops` ← `sim` ← `timetable`, and `sim` ← `kpi`. The visualization packages sit on top: `vizproto` knows only the world (`infra`), `schematic` and `traingraph` draw from `vizproto`, and only `record` knows about `sim`.

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
`Train` is identity, schedule and movement state: the `Route` its front is on and the `MovementAuthority` it runs under. Each `SchedulePoint` keeps the actual arrival and departure next to the timetabled ones, and `Train.Delay(now)` derives from them how late the train is running (growing while it waits past a timetabled time). It also records the platform actually used, the stops at signals on the way to it, and which train's leaving a track finally let the train go (`WaitedFor`), for the timetable. A `MovementAuthority` can only be created from a reserved `Route`, so everything it covers is already held for the train. It never runs past a signal. `NewMovementAuthority` covers the route up to the next signal or platform end ahead of the train, and `Extend` moves it on one signal section at a time.

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

`OnTrackReleased` wakes only the requests waiting on that track, highest rank first, then longest-waiting. A request's rank is its train's priority, raised by **ageing** for how long it has waited (see Priority).

**Schedules name every station.** `Sim.Init` rejects a train that would run through a station without a schedule point for it; a station a train does not stop at gets a pass point (`ops.NewPassPoint`, untimed with a negative time).

**Priority.** A train's priority is a number, higher served first: its base `Priority`, adjusted by the delay band it falls in. Both come from a `sim.PriorityConfig` (`Sim.Priorities`), loaded from JSON: `classes` maps a train's `Class` to its base priority, and `delay_bands` adjust it by lateness (the band with the largest `over_min` below the delay applies). The sim gives classes and thresholds no meaning of their own; `profiles/ir.json` holds Indian Railways' (VB Express highest down to Special Express; up to 15 min late is boosted as recoverable, over an hour is lowered as unrecoverable). Priority is worked out when it is needed, so it follows the delay as it grows, and `Sim.SetPriorityAt` changes a train's base priority during a run (logged as `PRIORITY_CHANGE`). `ageing` (`every_min`, `adjust`) raises a waiting request's rank by `adjust` for every `every_min` minutes it has waited, so no train is kept waiting for ever behind a stream of higher-priority ones; `profiles/ir.json` adds 5 every 10 minutes. Ageing only orders the queue: priority, precedence and the logged `priority` are unchanged. `min_stop_min` (default 1) is the shortest a train stands at a stop, even when it runs late or is timetabled for less; a pass never stops for it, nor does a halt in a timetable (a pass the train is timed to stand at).

**Precedence** (on when the config has a `precedence` block; see `docs/trainapp/precedence/plan.md` for the design). Before granting a train L anything, the dispatcher looks ahead (`projection.go`): each train's unimpeded future, at the track speed limits, and for a train already waiting on another's track, from when that track is due to be free. It then asks whether L going now would cost a higher-priority train H time, in one of three ways:
- **overtake**: H would catch up with L, both running the same way;
- **cross**: they would meet head on on single line, or at a switch or diamond;
- **platform**: L would take the station track H needs.

If so, and the hold rule agrees, L is **held** where it stands until H is past, as long as it stands somewhere H can get by it (a refuge). The rule is `thresholds` (priority gap and minutes saved, with `max_wait_ratio` capping how many minutes the held train may wait per minute the other saves, whatever the classes) or `weighted` (priority × minutes, saved against waited), picked by name, and `max_hold_min` caps how long a train is held at one place in all. At stations the **station plan** may move L to another track instead. Station tracks are given out highest priority first: through tracks are kept for trains running through, a stopping train may use the main line only for a stay up to `main_line_max_stay_min` (a terminus has no limit, since nothing runs through it), a train running through prefers a through track, then its main line, and between equal choices a train keeps to its own side of the station rather than crossing the other line. A train running through that is expected to stand at the station (to cross a train of any priority that gets onto the single line first, held for a higher one, waiting for a track at the next station, or because the line it leaves on is already taken) is planned onto a loop instead: standing on the main line or a through track is its last resort (`PLATFORM_CHANGE` reason `standing`). The one exception is a short stand, up to `main_line_max_stay_min`, on its own main line where the only loops lie across another line's main: crossing over and back would cost more than the stand.

**Reception.** A train ready to leave a station is **held** there (kind `reception`) if, going now, it would reach the next station that can take it off the line (halts, whose only track is the line, are looked past) with every track it could use still taken by trains that get there first: it would only stand at the home signal and shut the line behind it. A train that would follow it there does not count. It is not a priority decision, so the hold rule does not apply, and `max_reception_hold_min` (default `max_hold_min`) caps it separately; the refuge, cycle and safety checks still do, and as a last resort it may hold a train on a main line. Where a crossing or overtake with the same train is also held for, that is the hold placed. Platform moves are logged as `PLATFORM_CHANGE`, holds as `PRECEDENCE_HOLD` and `PRECEDENCE_RELEASE`, and a hold the rule wanted but could not place as `PRECEDENCE_SKIPPED` (with why: `no_refuge`, `unsafe`, `max_hold`, `main_line_limit`, `through_track`, `cycle`).

A hold must not fight deadlock avoidance: the safety check knows about every active hold (the held train can only finish after the other has reached its milestone, the first station past it), so no other grant can strand H behind L, and a hold that would make the state unsafe is never placed. With precedence on, the check also judges each train where it will stand once its granted route is run, and may move a train on a stop at a time when none can finish outright; both keep a move judged safe during a hold from looking unsafe once the hold ends. With precedence off the check is unchanged.

**Overrides.** Every decision has an id that stays the same from run to run (`hold:<train>><for>@<place>`, `pf:<train>@<station>`). `Sim.Override` / `Sim.OverrideAt` and `trainapp -overrides file.json` release (or veto) a hold, force one, pin a train's platform, or turn precedence off for a train. A forced hold holds the train at the station, once it is there, until the other train has left it, or with `"until": "arrived"` until the other has reached it (e.g. for a crossing). A pinned platform is the train's first choice in the station plan, but a track booked for a higher-priority train is still tried last. With `Sim.ManualPrecedence` precedence makes no decisions of its own: only the overrides hold trains and pick platforms, a pinned one first.

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

### 7. Timetable generation (`timetable`)
Give trains only an entry time, stop points with a dwell (`ops.NewStopPoint`) and untimed pass points (`ops.NewPassPoint(stn, -1, pf)`), run them, and their actual times are a timetable that works. The stations are through stations, so each train has an arrival at its first station (its entry, the one fixed time) and a departure from its last. Fully timed trains can share the run, so new paths can be fitted around an existing timetable.

- **`timetable.FromRun`** reads the times back. Arrivals and passes are when the front reaches the end of the platform track. A train that stood at a pass point gets a **halt**, and one that stood past its dwell an **extended dwell**, each with a reason: `cross` (waited for a train from the other way), `overtake` (a train behind passed it) or `technical` (anything else, e.g. a train ahead on the line). The reason comes from a precedence hold if there was one, and otherwise from the train whose leaving a track let it go and how the two schedules meet there. Stops at signals between stations, which hide inside the running time, are listed too. A train that could not enter on time (no free boundary platform) is a conflict and gets a warning: in timetable mode trains do not enter late.
- **`timetable.Round`** adds the recovery allowance to running times and rounds arrivals and departures **up** to the step, so no train is timed to leave before it could and no dwell gets shorter than its minimum. The entry time is kept as it is. Each running time starts from the rounded departure at a stop or halt, where a timed train waits for it, but from the unrounded time at a pass, which it runs through whenever it gets there, so rounding does not add up at every station passed. Rounding keeps the meetings: a train that stood at a station for another moves on by as much as that train's time there did (its arrival for one from the other way, its departure for one going the same way), and the trains are rounded again until no departure moves.
- **The check run** (`timetable.NewCheckSim`, `Report`) runs the rounded timetable again as fully timed trains and reports every train that runs later than the tolerance. With precedence on, the profile's `check_precedence` (or `-check-precedence`) says how it decides. `plan`, the default, keeps to the timetable's plan (`Timetable.PlanOverrides`): each train pinned to the track it used, and held at each crossing and overtake until the other train has arrived or left; precedence makes no decisions of its own (`Sim.ManualPrecedence`), since deciding afresh from the new times would move the meetings. This shows the timetable can be kept, not just that it was generated. `auto` lets precedence decide everything afresh, as it would on the day, so it shows whether the dispatching itself keeps the timetable. The report says which it was.

The settings are the `timetable` section of the profile: `rounding_min`, `recovery_pct`, `halt_min` (how long counts as standing) and `check_tolerance_min`. The result depends on the order trains get their paths (priority, then first come): it is one timetable that works, not an optimised one.

### 8. KPIs (`kpi`)
Every `trainapp` run ends with a KPI report, so a change to the dispatching can be shown to make a run better or worse on the objectives.

- **Delay** is measured against free-running time. `kpi.FreeRunning` runs each train again with the line to itself, taking the dispatcher's own way, platforms and dwells. A train's journey runs from its planned entry to when it leaves the world, so waiting to enter counts. The report gives the total, the mean, the mean weighted by base priority, the worst train, and punctuality: the share of trains within each of the profile's `punctual_within_min` (1, 5 and 15 min in `ir.json`). For timed trains, such as the check run's, it gives the same against the timetable: lateness at the last timed point, early counting as on time.
- **Waits** are each time a train stood longer than its schedule asked: to enter the world, at a signal, or at a station past its dwell (a precedence hold, say). The longest single wait is reported with its place.
- **Utilization** comes from a `kpi.Collector`, a `sim.Observer` that notes when each train's front entered a track, reached its far end, and left it. Each track's occupancy is split into *running* and *standing*, as shares of the window from the first train's entry to the last train's exit, with trains per hour. Tracks are blocks (outside every station's area), platform tracks, or throat tracks. Standing on a block is a train standing in section. Train length is not modelled, so a track counts as left once the front enters the next.
- **Disruptions** (`ops.Disruption`, `-disruptions`) make trains run worse than planned: `stand` keeps a train at a station for extra minutes, as for a failure, which the dispatcher learns of only once the train is there, and `late_entry` has it enter late. Their minutes are the primary delay; the rest of the run's delay is reactionary.

`trainbench` runs the scenarios in `scenarios/` under each precedence rule and prints the KPIs side by side; see [Run the benchmark](#run-the-benchmark).

### 9. Visualization (`visualization`, `cmd/trainviz`)
The simulation never knows it is being drawn. A `sim.Observer` can be set on the `Sim`; it is handed every log entry, and told after each event once the train's controller has run.

- **`vizproto`** is the stream between the two programs: newline-delimited JSON, one message per line, each with a type and the sim time. A `hello` describes the world, then `track`, `switch`, `train` and `event` messages report what changed, and the stream ends with `end` (or `error` if the run failed). It reports state changes rather than sim events, so the viewer does not depend on how the sim works inside.
- **`record`** is the `Observer` that writes that stream. After every event it compares the interlocking and the trains with what it last sent, and sends only the difference. Recording does not change the run or its log.
- **`schematic`** lays a world out as a line schematic: tracks run west to east along horizontal lanes, and switches and crossovers move between lanes. It uses the lane information the templates recorded (see Network model). Nothing is drawn to scale. It renders at three levels (overview, layout, detail) and can overlay a replayed state. `Diagram.Corridor` lays a single line's stations along a distance axis: each at the length of the shortest line from the westernmost, and every point on the line between two stations in proportion.
- **`traingraph`** draws a recording as a time-distance graph along the corridor, from the trains' recorded moves, with their planned times as markers.
- **`trainviz`** is the front end: it draws a world, or opens a recording with `replay`, using [Bubble Tea](https://github.com/charmbracelet/bubbletea). See [Draw a world](#draw-a-world) and [Replay a run](#replay-a-run).

## cmd/trainapp

[cmd/trainapp/main.go](cmd/trainapp/main.go) builds `worlds.BuildTpjKkdiWorld()`: three stations (TPJ, PDKT, KKDI) with four platforms each, connected by a double line (TPJ↔PDKT ~55km, PDKT↔KKDI ~41km) with crossovers at the termini and a signal at the arrival end of each leg. It adds the demo trains (`-demo` each way, 3 by default, 10 minutes apart), runs the simulation, and prints how many trains completed their schedule, e.g. `6/6 trains completed their schedule by t=107.18 (log: logs/logs.log)`, then the KPI report (see [KPIs](#8-kpis-kpi)).

| Flag | Default | Effect |
|---|---|---|
| `-scenario` | none | Run a scenario file (see [Run the benchmark](#run-the-benchmark)): its world, schedule (or timetable), profile and disruptions, in place of `-world`, `-schedule`, `-priorities` and `-disruptions`, which it refuses alongside |
| `-rule` | profile's | Precedence rule in place of the profile's: `thresholds`, `weighted`, or `off` to run without precedence |
| `-world` | `tpjkkdi` | World to run: `tpjkkdi` (the double line), `tpjkkdi-sl` (the real single line; the trains run through the stations between TPJ, PDKT and KKDI), `tpjkkdi-dl` (the real line doubled; needs a `-schedule`) or a world snapshot file (`.json`, see [World and schedule files](#world-and-schedule-files)) |
| `-schedule` | none | Trains to run (JSON); without it, the demo trains, which need `tpjkkdi` or `tpjkkdi-sl` |
| `-demo` | `3` | With no `-schedule`, how many demo trains run each way |
| `-disruptions` | none | Disruptions to apply to the trains (JSON): extra stands at stations, late entries; see [KPIs](#8-kpis-kpi) |
| `-kpi` | none | Also write the KPI report to this file (JSON): the run's, and with `-timetable` the check run's |
| `-save-world` | none | Also write the world to this file as a snapshot, which `-world` can load |
| `-save-schedule` | none | Also write the trains' schedules to this file, which `-schedule` can load |
| `-log` | `text` | Log format: `text` (logfmt, `logs/logs.log`) or `json` (`logs/logs.jsonl`) |
| `-record` | none | Also write the run to this file, for `trainviz replay` |
| `-priorities` | `profiles/ir.json` | Priority config (classes, delay bands and precedence); empty for none, which leaves every train at priority 0 and precedence off |
| `-overrides` | none | Precedence overrides to apply (see [Precedence](#5-dispatcher-and-deadlock-avoidance-sim)) |
| `-timetable` | none | Generate a timetable from the run, check it with a second run, print it and write it to this file (JSON); see [Timetable generation](#7-timetable-generation-timetable) |
| `-check-record` | none | With `-timetable`, also record the check run for `trainviz replay` |
| `-check-precedence` | profile's | With `-timetable`, how the check run decides precedence: `plan` (keep to the timetable's plan) or `auto` (decide afresh) |

The other worlds in `worlds/` are not run by `trainapp`. The single-line and junction worlds are used by the deadlock tests, and all of them can be drawn with `trainviz`.

`worlds.BuildTpjKkdiSingleLineWorld()` (`trainviz -world tpjkkdi-sl`) is the real line between the same two junctions: a single electrified line at 110 km/h with all ten stations (TPJ, KRMG, KRUR, VEL, PDKT, NMN, TYM, CTND, KTYR, KKDI), each stretch between stations one block section. Station positions and track layouts come from OpenStreetMap: crossing stations with the main line straight through between two loop platforms, PDKT and CTND with a platform on the main line and a goods loop, two halts, and TPJ (8 platforms and a through track) and KKDI (5 platforms and a through loop) at the world's edges, their throats laid as ladders of 15 km/h turnouts. Platform numbers OSM does not record are guesses. A halt's platform ends act as signals, so unlike on the real line a halt splits its block section.

`worlds.BuildTpjKkdiDoubleLineWorld()` (`trainviz -world tpjkkdi-dl`) is an imaginary doubling of that line: the same stations at the same km, joined by a double line with left-hand running (the upper line east, TPJ to KKDI), each line signalled one way and each stretch one block section per line. KRUR, VEL and TYM have a loop platform off each line's through main; KRMG has PF1 on the east main and a loop off the west line's through track; PDKT and CTND have PF1 and PF2 on the mains and PF3 and a goods loop off the west line; NMN and KTYR have a platform on each line. TPJ, KRMG, PDKT, CTND and KKDI have a scissors crossover between the lines in each throat. See docs/trainapp/double_line/plan.md.

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

### Generate a timetable

```bash
go run ./cmd/trainapp -timetable tt.json -record run.ndjson -check-record check.ndjson
```

The trains in `main.go` have only entry times, dwells and preferred platforms. This prints each train's timetable and the check run's verdict, and writes the same as JSON:

```text
0453U  Train3Up  (Mail/Express)
  STN   KIND  PF  ARR    DEP    DWELL  RUN  NOTES
  TPJ   stop  2   00:41  01:43  62     —    dwell +50.8 (overtaken by 0454U)
  PDKT  stop  1   02:15  02:20  5      32
  KKDI  stop  1   02:44  02:54  10     24

check run: all 12 trains kept their times (tolerance 1 min, precedence plan)
```

Times are minutes from 0, shown as a clock. Open `check.ndjson` with `trainviz replay` and press `g`, then `p`: the planned times sit on the trains' lines.

A timetable goes back into a scenario as its `timetable` (a path, like the schedule's), for planning against it: disrupting it, trying the rules on it.

```json
{"name": "sl-12-tt", "world": "tpjkkdi-sl", "timetable": "timetables/sl-12.json", "profile": "../profiles/ir.json",
 "disruptions": [{"train": "0452U", "kind": "stand", "stn": "PDKT", "min": 30}]}
```

Such a scenario runs the timetable's timed trains in place of a schedule (the schedule, the draft the timetable came from, may stay or go). Under the profile's rule it keeps to the timetable's plan, as the check run does (`check_precedence`: each train on its platforms, held for the trains it waits for in the timetable); a named rule (`-rule thresholds`, `weighted`) decides precedence afresh, and `off` runs without. trainedit's Run tab exports a timetable (`t`) and makes the scenario run it (`u`); see [Edit a scenario](#edit-a-scenario).

### Measure a run

Every run prints its KPIs after the summary; `-kpi` writes them as JSON too:

```bash
go run ./cmd/trainapp -world tpjkkdi-sl -kpi kpi.json
go run ./cmd/trainapp -world tpjkkdi-sl -disruptions disruptions.json
```

```text
KPIs, run: 6/6 trains finished, t=1.0-106.0
  delay against free running: 37.3 min in all, 6.2 a train, 5.6 priority-weighted; worst 0451D 9.1 min
    punctual: 0% within 1 min, 33% within 5 min, 100% within 15 min
  waits: 33.5 min in all; longest 0451D 8.3 min at VEL (station, from t=45.3)
  precedence: 2 holds lasting 11.1 min in all, 2 platform changes
  blocks: 28% occupied on average (14-45%), 0.0% standing (0.3 min in section)
            block  occupied  running  standing  trains/h
       slTpjKrmg0       37%      37%      0.0%       3.4
  ...
```

A disruptions file lists the trains to disrupt:

```json
{"disruptions": [
  {"train": "0452U", "kind": "stand", "stn": "PDKT", "min": 30},
  {"train": "0453D", "kind": "late_entry", "min": 15}]}
```

### Run the benchmark

```bash
make bench                                            # every scenarios/*.json under thresholds, weighted and off
go run ./cmd/trainbench -rules weighted scenarios/sl-12.json
```

A scenario names a world (`tpjkkdi`, `tpjkkdi-sl` or a snapshot), a schedule file (or a timetable, see [Generate a timetable](#generate-a-timetable)), a profile and any disruptions, with paths relative to the scenario file. The ones included are the single line with 6, 12 and 24 trains (`sl-6`, `sl-12`, `sl-24`), the double line with 12 (`dl-12`), the real line doubled with 10 and 20 (`dl-10`, `dl-20`, no timetable), and `sl-12-disrupt`, with one train failing for 30 min at PDKT and another entering 15 min late. `trainbench` runs them in parallel (`-j`), prints one row per scenario and rule, and writes every report to `logs/bench.json`; each run's log is under `logs/bench/<scenario>-<rule>/`. The 24-train runs take a couple of minutes each.

### World and schedule files

The world and the trains can come from files instead of the code. Save a built-in world and the demo trains to start from:

```bash
go run ./cmd/trainapp -world tpjkkdi-sl -save-world sl.json -save-schedule trains.json
go run ./cmd/trainapp -world sl.json -schedule trains.json   # the same run, from the files
go run ./cmd/trainviz -world sl.json                         # draw it
```

A **world file** is a snapshot of the world (`infra.Snapshot`): every point, track, switch block, signal and station platform as built, so a world comes back from it unchanged and runs exactly as before. Points are given by the parts of their id (`stn`, `type`, `seq`, `other`; `stn` puts the point in that station's area), and are named in the rest of the file by their id's string, e.g. `tpjPf1S`. A track runs `from` its west end `to` its east end and is named by its two point ids, sorted and joined by `-`, as in the logs. The `id`s Snapshot writes may be left out; if given, they are checked. Lengths are in meters, speeds in km/h, and `defaults` are what tracks added later get. A switch block lists its tracks (only one in use at a time, and no running from one onto another through a point they share) and the one it starts set to; switches are listed in the order they were built, which is the order the interlocking checks them in. A platform of length 0 is a through track. Loading reports every problem it finds, then finalizes the world.

```json
{"version": 1,
 "defaults": {"switch_kmph": 30, "switch_length_m": 100, "platform_kmph": 50, "platform_length_m": 800, "route_kmph": 110},
 "points": [{"id": "tpjPf1S", "stn": "TPJ", "type": "Pf", "seq": "1", "other": "S", "dead_end": true, "boundary": true}, ...],
 "tracks": [{"id": "tpjPf1E-tpjPf1S", "from": "tpjPf1S", "to": "tpjPf1E", "length_m": 800, "max_kmph": 50}, ...],
 "switches": [{"id": "tpjSw0", "tracks": ["tpjBp0-tpjPf2E", "tpjBp0-tpjPf1E"], "default": "tpjBp0-tpjPf2E"}, ...],
 "signals": [{"id": "tpjSig0", "at": "tpjBp3", "facing": "pdktBp1"}, ...],
 "stations": [{"code": "TPJ", "name": "Tiruchy Jn", "platforms": [{"no": "1", "track": "tpjPf1E-tpjPf1S", "length_m": 600}, ...]}, ...]}
```

A **schedule file** lists the trains (`ops.LoadTrains`). A stop gives either a `dwell`, and the train leaves once it has stood that long, or an `arr` and a `dep` to keep to, with an optional `min_dwell` it stands even when late. A pass may give an `arr`, when it is due through; without one it is untimed. `pf` is the preferred platform; left out, the dispatcher picks. The first point's `arr` is when the train enters the world, and is required. `class` must be one of the priority config's classes.

```json
{"trains": [{"number": "0451U", "name": "Train1Up", "class": "Superfast Express", "max_speed_kmph": 110, "points": [
  {"stn": "TPJ", "kind": "stop", "pf": "1", "arr": 1, "dwell": 5},
  {"stn": "KRMG", "kind": "pass"},
  {"stn": "PDKT", "kind": "stop", "arr": 40, "dep": 42, "min_dwell": 1}, ...]}]}
```

### Draw a world

```bash
go run ./cmd/trainviz -world tpjkkdi         # full screen: pan and zoom
go run ./cmd/trainviz -print                 # print once
go run ./cmd/trainviz -print -zoom 2         # print once, wider
```

`-world` is one of `tpjkkdi`, `tpjkkdi-sl`, `tpjkkdi-dl`, `test`, `single` or `junction`, or a world snapshot file (`.json`). With `-print`, `-zoom 1` is the most compact, `-level` is `overview` (stations collapsed to boxes), `layout` or `detail` (with point, track and switch ids), and `-color` is `auto`, `always` or `never`. The schematic is not to scale: lines between stations are labelled with their length instead.

In the viewer, `+`/`-` or the mouse wheel zoom, `hjkl`, the arrows or a mouse drag pan, `/` then a station code jumps to it, `0` fits the world on screen, and `q` quits.

```text
  TPJ                    PDKT                   KKDI
[═Pf1═══               ══Pf1═══               ══Pf1══]
        ╲             ╱        ╲             ╱
[═Pf2═══─────(55km)─▶──══Pf2═══───(41km)─▶────══Pf2══]
          ╳                                ╳
[═Pf3═══────◀(55km)────══Pf3═══──◀(41km)──────══Pf3══]
        ╱             ╲        ╱             ╲
[═Pf4═══               ══Pf4═══               ══Pf4══]
```

### Replay a run

```bash
go run ./cmd/trainapp -record run.ndjson
go run ./cmd/trainviz replay run.ndjson
go run ./cmd/trainviz replay -print -at 34.5 run.ndjson   # one frame, as text
go run ./cmd/trainviz replay -print -graph run.ndjson     # the time-distance graph, as text
```

The recording is newline-delimited JSON (`visualization/vizproto`): a `hello` with the world, then after each sim event the log entries and every track, switch and train that changed. Recording does not change the run or its log. With `-print`, `-at` picks the sim time to show (the end of the run by default), and the frame is followed by a list of the trains and what each waits for.

Train numbers in the side pane are coloured by class, from blue for the lowest priority class to red for the highest, with a key below the trains (the recording carries each train's priority as it goes). `s` swaps the events pane for the selected train's schedule: a column per station, with whether it stops or passes, and the platform and times it actually arrived and left; times still to come are dimmed.

Tracks are grey when free, green when reserved and red when occupied. A switch leg that is not set is drawn faint. Trains are labelled at their front, e.g. `0451U▶`. Signals (`▶ ◀`, also drawn in the plain world view) show green when the track beyond is reserved for the approaching train, and red otherwise. The side pane lists each train's phase and what it waits for, and the events pane shows the latest log entries.

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
| `s` | show the selected train's schedule instead of the events |
| `g` | switch between the schematic and the time-distance graph |

The pan, zoom, `/` and `q` keys work in the replay too. Clicking on the timeline seeks to that time. `ctrl+c` quits with exit status 130 rather than 0, so a launcher such as `traindemo` can tell it from `q`.

**Time-distance graph.** `g` swaps the schematic for a train graph: time left to right, the line's stations top (west) to bottom (east) at their distances, each train a line, sloping while it runs and flat while it stands. Lines are drawn up to the replay time, marked by a cursor, so the graph builds as the run plays; up and down trains have their own colour, and the selected train is picked out. There, `+`/`-` or the wheel change the minutes per column, `h`/`l` or a drag pan in time, `0` fits the whole run, `p` marks the trains' planned times (`◆`), and a click seeks to that time. The graph needs a single line, so the junction world has none. With `-print -graph`, `-width`, `-height`, `-planned` and `-at` shape the printout.

### Demo the scenarios

```bash
make demo        # builds dist/trainapp, dist/trainviz, dist/trainedit and dist/traindemo, then runs traindemo
```

`traindemo` lists the scenarios in `scenarios/` (`-dir`) and previews the selected one: its description, the world's schematic (`←` `→` pan, `z` switches layout and overview), and a row per train with its class, entry time, route, stops with their planned stop times and any disruption, the classes coloured by priority as trainviz colours them. `tab` moves the arrow keys to the trains (`esc` back): the picked train's timetable shows beside the schedule, a row per station with its platform, arrival, departure, and planned and actual stop; once the scenario has run under the selected rule, the times and platforms are the run's, and a stop longer than planned is amber. Every precedence rule is shown (the profile's, `thresholds`, `weighted`, `off`), with what the selected one does under the profile's settings; `r` and `R` move between them. `enter` runs the scenario with `trainapp -scenario ... -rule ... -record ... -kpi ...`, drawing its progress in place (`esc` cancels), then sums up the run (trains done, delay, punctuality, reactionary delay, holds). `v` replays it in `trainviz replay`; `q` there comes back to the list, and `ctrl+c`, there or in the list, quits everything. Each run is written to `logs/demo/<scenario>-<rule>.ndjson` and `.kpi.json` (`-out`), and runs from earlier sessions are read back from there, so `v` replays them too; one older than its scenario's files shows as stale ("ran before the last edit"). `e` opens the selected scenario in trainedit and `n` starts a new one from it (see [Edit a scenario](#edit-a-scenario)); when trainedit quits, the list is read again, the saved scenario selected. A scenario file that does not read is listed with its error (`✗`), so `e` can open it to put it right. A scenario running a timetable shows its timed trains.

traindemo does not link the sim: it runs the `trainapp`, `trainviz` and `trainedit` binaries, looking next to itself, then in `dist/`, then on `$PATH` (`-bin` names the directory).

### Edit a scenario

```bash
make edit SCENARIO=scenarios/sl-12.json     # or e in traindemo
make edit                                   # a new scenario, or n in traindemo
go run ./cmd/trainedit -new -world tpjkkdi-sl
```

`trainedit` has three tabs (`1` `2` `3`): the world, the trains and the scenario. `ctrl+s` saves them all, `q` quits (asking about unsaved changes), and `ctrl+c` quits at once.

- **World** draws the world on its schematic, as trainviz does, from its snapshot as it stands, so a half-built world shows too. The arrows move from piece to piece (point, track, signal, station) and `tab` steps through the pieces in one cell; `+` `-` `0`, `shift`+arrows and the mouse zoom and pan, and `/` goes to a station. Letters edit what is picked: `e` extends track from a point (or starts an empty world), `c` splits a track, `j` joins the two tracks at a point, `s` makes a point a switch, `x` (on two tracks in turn) lays a crossover (single, either way, or scissors), `X` makes two tracks cross at a diamond or a single or double slip, `b` (on two points) lays a signalled line between them running east, west or both ways (or, asked to, a line from each open end beside one to each beside the other, running as a pattern says, e.g. `E W` for a double line), `Y` leaves the line at the picked track for a branch (a switch off each of the line's tracks a branch line leaves, and a diamond where a leg crosses another: a flat junction), `g` and `G` put a signal at a track's east or west end, or take it away, `o` makes a point open, a buffer stop or the world's edge, `p` makes a track a platform, `n` adds or renames a station, `t` opens the station builder (a table of the station's tracks, a row a track top to bottom: its kind, number, length, speed, whether it is a main west and east, which main a track between two hangs off, a siding's buffer stop; rows for the crossovers between neighbouring mains each side; the station drawn beside it as it stands, and what is wrong with it said as it is edited; starting from a blank, a halt, a crossing station or a double-line station; `ctrl+o` adds a track, `ctrl+k` removes one, `alt+↑` `alt+↓` move it, and `:` gives the tracks as text, e.g. `1:600 9 2:600 5:600* ~Gd1:700`, to type or paste) and stamps it, joined to the picked point, `a` sets a track's name, length, speed and lanes, `D` the world's defaults (and which way UP runs), and `d` deletes. A station on several lines has a main for each: each main's line meets its own ladder of switches, a track between two mains hangs off either or both (the default for one in the middle), the mains each side can differ (a junction station), and the throats can have crossovers between neighbouring mains. Stamped, it joins only the picked point, and its other lines are drawn with `b`, unless asked to join every open end beside it. The sim takes a line's way from its signals; trainedit labels a one-way line UP or DOWN by the world's UP and shows it with the length, and lists a line whose way changes along it, or whose label its signals do not bear out, as a problem. A station added with `n` has no platforms, so it is not drawn: `/` still picks it, and `t` with it picked builds its tracks into it (`d` deletes it). In a form's or a table's text, `←` `→` `home` `end` move the cursor, and typing, `backspace` and `delete` work at it; in a choice, `←` `→` choose and typing picks the choice that starts with what is typed. Dragging from an open point draws track (its end sets the direction and lanes). `u` undoes and `ctrl+r` redoes. An edit that would leave a world that cannot be drawn (a track running east to west, lanes that do not add up) is refused; anything else wrong (an open track end, what `Snapshot.Build` reports) is listed under problems, marked red on the schematic and visited with `[` `]`: such a world saves, but does not run. A built-in world is saved as a snapshot beside the scenario (`scenarios/worlds/<name>.json`) the first time it changes.
- **Schedules** lists the trains, and the picked one's points (`tab`), checked as they change against the schedule file's rules and the world. `a` adds a train, `f` re-routes one and `g` adds a pattern of trains, each in the route picker: from and to, the ways a train can take between them (a train never reverses, so it runs one way all along), shortest first, the chosen one drawn on the schematic beside it, and its stations as a table, each a stop or a pass, with a dwell and a platform picked from the station's (a pattern's trains back stop at the same stations, on any platform). A pattern runs every so many minutes, both ways if asked, its classes in turn, numbered like `045{n}U`. `c` copies a train later, `s` sorts by entry, `x` disrupts one (a stand or a late entry) and `X` clears its disruptions; on the points, `enter` edits one, `i` inserts, `d` deletes and `K` `J` move it.
- **Run** shows the scenario's name, description, profile and files (`e` edits a field), the precedence rules (`r` `R`) and runs it as traindemo does (`enter`), from a scratch copy if there are unsaved changes; `v` replays the run. `t` runs it and exports its timetable with its check run, `u` makes the scenario run that timetable (copied to `scenarios/timetables/<name>.json`), and `U` goes back to the schedule.

Saving a world or schedule another scenario uses too (`sl-12` and `sl-12-disrupt` share `schedules/sl-12.json`) asks whether the change is for all of them or for a copy of this scenario's own. Runs go to `logs/demo/` as traindemo's do, so a run made in either shows in the other.

### Run the tests

```bash
go test ./...
```

The deadlock tests (`railway/sim`) run trains over the single-line and junction worlds, and the other packages test their own pieces (graph search, interlocking, movement authority, the event engine, the recorder, the schematic and the viewer).

### Build the programs

```bash
go build -o trainapp ./cmd/trainapp
go build -o trainviz ./cmd/trainviz
make build-traindemo   # all four into dist/
```

## Development

See [TODO.md](TODO.md) for planned work and improvements.

## License

See [LICENSE](LICENSE) for the license text.
