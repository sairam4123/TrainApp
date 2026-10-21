// Package report holds a run's KPIs as trainapp -kpi writes them, for the
// apps that read them back (trainviz, traindemo) without linking the sim.
// Package kpi works them out.
package report

// Track kinds.
const (
	KindBlock    = "block"    // a running line between stations, outside every station's area
	KindPlatform = "platform" // a station's platform track, loops and main lines alike
	KindThroat   = "throat"   // any other track in a station's area: points and approaches
)

// Utilization is how much of the run each track was occupied, from the
// first train entering the world to the last leaving it (the window).
type Utilization struct {
	FromMin   float64      `json:"from_min"`
	ToMin     float64      `json:"to_min"`
	Blocks    Summary      `json:"blocks"`
	Platforms Summary      `json:"platforms"`
	Stations  []StationUse `json:"stations"`
	Tracks    []TrackUse   `json:"tracks"`
	// Bands are the profile's capacity_color_band, lowest first, which
	// trainviz colours the tracks by.
	Bands []CapacityBand `json:"capacity_color_band,omitempty"`
}

// Band is the band an occupancy, a share of the window, falls in, as an
// index into Bands; -1 if it is under every band.
func (u Utilization) Band(occupancy float64) int {
	return bandOf(len(u.Bands), func(i int) float64 { return u.Bands[i].OverPct }, 100*occupancy)
}

// Summary sums up the tracks of one kind. Occupancies are shares of the
// window, 0 to 1.
type Summary struct {
	Tracks        int     `json:"tracks"`
	MeanOccupancy float64 `json:"mean_occupancy"`
	MinOccupancy  float64 `json:"min_occupancy"`
	MaxOccupancy  float64 `json:"max_occupancy"`
	// MeanStanding is the share of the window a train stood on one of the
	// tracks, on average; StandingMin the minutes trains stood on them in
	// all. On blocks, that is trains standing in section.
	MeanStanding float64 `json:"mean_standing"`
	StandingMin  float64 `json:"standing_min"`
}

// StationUse is how busy a station's platform tracks were.
type StationUse struct {
	Stn       string  `json:"stn"`
	Platforms int     `json:"platforms"`
	Occupancy float64 `json:"occupancy"` // mean over its platform tracks
	// StandingMin is the minutes trains stood on its platform tracks: their
	// dwells and their waits.
	StandingMin float64 `json:"standing_min"`
}

// TrackUse is how one track was used. A train is running on it until its
// front reaches the far end, then standing until it leaves.
type TrackUse struct {
	Track         string  `json:"track"`
	Kind          string  `json:"kind"` // KindBlock, KindPlatform or KindThroat
	Stn           string  `json:"stn,omitempty"`
	Pf            string  `json:"pf,omitempty"`
	Entries       int     `json:"entries"`
	OccupiedMin   float64 `json:"occupied_min"`
	RunningMin    float64 `json:"running_min"`
	StandingMin   float64 `json:"standing_min"`
	Occupancy     float64 `json:"occupancy"`
	Running       float64 `json:"running"`
	Standing      float64 `json:"standing"`
	TrainsPerHour float64 `json:"trains_per_hour"`
	// Section is the section a block is part of, named as Loss.At; empty
	// for other tracks.
	Section string `json:"section,omitempty"`
	// Split is what the occupied minutes went on, by cause: CauseFree and
	// CauseDwell are needed, the rest lost. They sum to OccupiedMin.
	Split []Share `json:"split,omitempty"`
}

// Report is a run's KPIs.
type Report struct {
	Trains     int      `json:"trains"`
	Finished   int      `json:"finished"`
	Unfinished []string `json:"unfinished,omitempty"`
	// Delay is against free-running time; nil without Options.Free.
	Delay *Stats `json:"delay,omitempty"`
	// Lateness is against the timetable, for timed trains; nil if none.
	Lateness *Stats `json:"lateness,omitempty"`
	// Disruption splits the delay; nil without disruptions.
	Disruption *Split `json:"disruption,omitempty"`
	// LongestWait is the longest single wait of any train; nil if none.
	LongestWait *Wait   `json:"longest_wait,omitempty"`
	WaitMin     float64 `json:"wait_min"` // every train's waits, in all
	// Holds and PlatformChanges are precedence's decisions; HeldMin is how
	// long the holds lasted in all.
	Holds           int         `json:"holds"`
	HeldMin         float64     `json:"held_min"`
	PlatformChanges int         `json:"platform_changes"`
	Utilization     Utilization `json:"utilization"`
	PerTrain        []Train     `json:"per_train"`
	// DelayBands are the profile's delay_color_band, lowest first, which
	// trainviz colours the trains' delays by.
	DelayBands []DelayBand `json:"delay_color_band,omitempty"`
	// StationDelays are how late the trains were at each station, in line
	// order; BlockDelays the delay they gained on each block. Empty without
	// free-running times.
	StationDelays []StationDelay `json:"station_delays,omitempty"`
	BlockDelays   []BlockDelay   `json:"block_delays,omitempty"`
	// ParetoPct is the profile's pareto_pct: the share of the minutes, in
	// percent, a Pareto chart marks off its vital few at. 0 in older files;
	// see Pareto.
	ParetoPct float64 `json:"pareto_pct,omitempty"`
}

// DefaultParetoPct is where a Pareto chart is cut without pareto_pct.
const DefaultParetoPct = 80

// Pareto is ParetoPct, or DefaultParetoPct if the file has none.
func (r *Report) Pareto() float64 {
	if r.ParetoPct > 0 {
		return r.ParetoPct
	}
	return DefaultParetoPct
}

// Causes: why a train lost time, or what a track's occupied time went on.
const (
	CauseDisruption = "disruption"   // primary delay: Kind is the disruption's kind
	CauseHeld       = "held"         // a precedence hold: Kind is the hold's kind
	CauseSignal     = "signal stop"  // stopped at a signal, not held
	CauseStation    = "station wait" // standing past the dwell, not held
	CauseEntry      = "entry"        // waiting to enter the world, not held
	CauseRunning    = "running"      // a delay: slower running (Kind RunSection) or a longer dwell (RunDwell) than running free

	// for occupancy only
	CauseFree   = "free running"   // the time running free takes: needed
	CauseDwell  = "dwell"          // the planned dwell: needed
	CauseSlower = "slower running" // running, or standing on the line, beyond that
)

// Kinds of CauseRunning.
const (
	RunSection = "section"
	RunDwell   = "dwell"
)

// Where a loss happened.
const (
	WhereStation = "station"
	WhereSection = "section"
	WhereEntry   = "entry"
)

// Loss is a piece of a train's delay: where, why, and how many minutes. A
// finished train's losses sum to its DelayMin; Min is negative where it
// made up time.
type Loss struct {
	Cause string `json:"cause"`
	// Kind is the hold's or the disruption's kind, e.g. "overtake" or
	// "stand"; for CauseRunning, RunSection or RunDwell.
	Kind string `json:"kind,omitempty"`
	// For is the train waited for, or made way for.
	For   string `json:"for,omitempty"`
	Where string `json:"where"` // WhereStation, WhereSection or WhereEntry
	// At is the station, the section ("PDKT–KKDI" on a single line, else
	// "PDKT→KKDI"), or for an entry the station entered at.
	At string `json:"at"`
	// Track is the track it happened on, by name; empty for an entry.
	Track   string  `json:"track,omitempty"`
	FromMin float64 `json:"from_min"`
	Min     float64 `json:"min"`
}

// Share is a part of a track's occupied minutes, by cause.
type Share struct {
	Cause string  `json:"cause"`
	Kind  string  `json:"kind,omitempty"`
	Min   float64 `json:"min"`
}

// Call is a train at one point of its schedule. Times are -1 and the
// measures nil where the train never got there, and the delays nil without
// free-running times.
type Call struct {
	Stn    string  `json:"stn"`
	Pass   bool    `json:"pass,omitempty"`
	ArrMin float64 `json:"arr_min"`
	DepMin float64 `json:"dep_min"`
	// DelayMin is how much later than running free the train was here: on
	// arriving, but at its first point on leaving, as arriving there is
	// getting onto the line.
	DelayMin *float64 `json:"delay_min,omitempty"`
	// ApproachMin is the delay it gained on the way here from its last
	// point (at its first, getting onto the line), and StationMin the delay
	// it gained here, from arriving to leaving.
	ApproachMin *float64 `json:"approach_min,omitempty"`
	StationMin  *float64 `json:"station_min,omitempty"`
	// LateMin is how late it was here against the timetable, early counting
	// as on time; nil where the timetable gives no time.
	LateMin *float64 `json:"late_min,omitempty"`
}

// StationDelay is how late the trains were at one station.
type StationDelay struct {
	Stn string `json:"stn"`
	// Calls are the calls measured, Stops and Passes of them.
	Calls  int `json:"calls"`
	Stops  int `json:"stops"`
	Passes int `json:"passes"`
	// Punctual is the share of the calls no later than each of the
	// thresholds, as for the run.
	Punctual []Punctuality `json:"punctual"`
	MeanMin  float64       `json:"mean_min"`
	Worst    string        `json:"worst,omitempty"`
	WorstMin float64       `json:"worst_min"`
	// ApproachMin and StationMin are the delay gained on the way here and
	// standing here, by all the trains.
	ApproachMin float64 `json:"approach_min"`
	StationMin  float64 `json:"station_min"`
	// LateCalls are the calls the timetable gives a time for, and
	// LatePunctual the share of them on time against it.
	LateCalls    int           `json:"late_calls,omitempty"`
	LatePunctual []Punctuality `json:"late_punctual,omitempty"`
}

// BlockDelay is the delay trains gained on one block: how much longer they
// took over it than running free.
type BlockDelay struct {
	Track     string  `json:"track"`
	Trains    int     `json:"trains"`
	GainedMin float64 `json:"gained_min"`
	Worst     string  `json:"worst,omitempty"`
	WorstMin  float64 `json:"worst_min"`
}

// DelayBand is the band a delay in minutes falls in, as an index into
// DelayBands; -1 if it is under every band.
func (r *Report) DelayBand(min float64) int {
	return bandOf(len(r.DelayBands), func(i int) float64 { return r.DelayBands[i].OverMin }, min)
}

// Stats sums up the delays of the finished trains it counts.
type Stats struct {
	Trains   int     `json:"trains"`
	TotalMin float64 `json:"total_min"`
	MeanMin  float64 `json:"mean_min"`
	// WeightedMeanMin weights each train's delay by its base priority (all
	// alike if no train has one).
	WeightedMeanMin float64       `json:"weighted_mean_min"`
	Punctual        []Punctuality `json:"punctual"`
	// Worst is the train with the largest delay, empty if none has any.
	Worst    string  `json:"worst,omitempty"`
	WorstMin float64 `json:"worst_min"`
}

// Punctuality is the share of trains, 0 to 1, no later than WithinMin.
type Punctuality struct {
	WithinMin float64 `json:"within_min"`
	Trains    int     `json:"trains"`
	Share     float64 `json:"share"`
}

// Split is the delay against free-running time, split into the disruptions'
// own minutes and the rest.
type Split struct {
	PrimaryMin     float64 `json:"primary_min"`
	ReactionaryMin float64 `json:"reactionary_min"`
}

// Wait kinds.
const (
	WaitEntry   = "entry"   // waiting to enter the world
	WaitSignal  = "signal"  // stopped at a signal
	WaitStation = "station" // standing at a station past its dwell, e.g. held for another train
)

// Wait is a train standing longer than its schedule asks.
type Wait struct {
	Train   string  `json:"train"`
	Kind    string  `json:"kind"`
	At      string  `json:"at"` // the station, or for a signal the track stood on
	FromMin float64 `json:"from_min"`
	Min     float64 `json:"min"`
	// Cause is why: CauseHeld, CauseSignal, CauseStation or CauseEntry, by
	// most of its minutes; For the train it waited for or made way for.
	Cause string `json:"cause,omitempty"`
	For   string `json:"for,omitempty"`
}

// Train is one train's KPIs. Times are -1 and the measures nil where the
// train never got that far.
type Train struct {
	Train    string `json:"train"`
	Class    string `json:"class,omitempty"`
	Priority int    `json:"priority"`
	Finished bool   `json:"finished"`
	// EntryMin is the planned entry, before any disruption.
	EntryMin   float64  `json:"entry_min"`
	ExitMin    float64  `json:"exit_min"`
	JourneyMin float64  `json:"journey_min"`
	FreeRunMin *float64 `json:"free_run_min,omitempty"`
	DelayMin   *float64 `json:"delay_min,omitempty"`
	LateMin    *float64 `json:"late_min,omitempty"`
	PrimaryMin float64  `json:"primary_min,omitempty"`
	WaitMin    float64  `json:"wait_min"`
	Waits      []Wait   `json:"waits,omitempty"`
	Calls      []Call   `json:"calls,omitempty"`
	// Losses split DelayMin by place and cause; empty without it.
	Losses []Loss `json:"losses,omitempty"`
}

// CapacityBand is how busy a track is that was occupied OverPct percent of
// the run or more, up to the next band.
type CapacityBand struct {
	OverPct float64 `json:"over_pct"`
	Name    string  `json:"name"`
	// Color is an ANSI 256-colour number, e.g. "42"; empty for trainviz's
	// own, by the band's place in the list.
	Color string `json:"color,omitempty"`
}

// DelayBand is how late a train is that was OverMin minutes late or more,
// up to the next band. It only names and colours the delay, unlike the
// sim's delay bands, which change priorities.
type DelayBand struct {
	OverMin float64 `json:"over_min"`
	Name    string  `json:"name"`
	Color   string  `json:"color,omitempty"` // as CapacityBand's
}

// bandOf is the last of n bands, lowest first, whose start v reaches; -1 if
// it is under them all.
func bandOf(n int, from func(int) float64, v float64) int {
	band := -1
	for i := range n {
		if v >= from(i)-1e-9 {
			band = i
		}
	}
	return band
}
