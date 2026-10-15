// Package vizproto is the stream trainapp writes and trainviz reads:
// newline-delimited JSON, one message per line, each with a type and the sim
// time t in minutes. The sim reports state changes (track holders, switch
// positions, train moves) rather than its events, so the viewer never depends
// on how the sim works inside.
package vizproto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// Version 2 added precedence: decision messages, the trains a waiting train
// is held for, and each platform's class. Version 3 added each train's class
// and planned schedule, version 4 its priority. Older recordings still read.
const Version = 4

const (
	TypeHello  = "hello"
	TypeTrack  = "track"
	TypeSwitch = "switch"
	TypeTrain  = "train"
	TypeEvent  = "event"
	// TypeDecision is a precedence decision as it is now.
	TypeDecision = "decision"
	TypeEnd      = "end"
	TypeError    = "error"
)

// Message is one line of the stream.
type Message interface {
	Head() *Header
	kind() string
}

type Header struct {
	Type string  `json:"type"`
	T    float64 `json:"t"`
}

func (h *Header) Head() *Header { return h }

// HelloMsg starts the stream and describes the world.
type HelloMsg struct {
	Header
	Version int   `json:"version"`
	World   World `json:"world"`
}

// TrackMsg is who holds a track now; both empty means free.
type TrackMsg struct {
	Header
	Track      string `json:"track"`
	ReservedBy string `json:"reservedBy,omitempty"`
	OccupiedBy string `json:"occupiedBy,omitempty"`
}

// SwitchMsg is the track a switch block is set for and who has it locked.
type SwitchMsg struct {
	Header
	Block    string `json:"block"`
	SetFor   string `json:"setFor"`
	LockedBy string `json:"lockedBy,omitempty"`
}

// TrainMsg is where a train's front is and what it is doing. The front runs
// Track from From to To between TStart and TEnd, then stays at To. Track is
// empty while the train is outside the world.
type TrainMsg struct {
	Header
	Train   string   `json:"train"`
	Track   string   `json:"track,omitempty"`
	From    string   `json:"from,omitempty"`
	To      string   `json:"to,omitempty"`
	TStart  float64  `json:"tStart"`
	TEnd    float64  `json:"tEnd"`
	Phase   string   `json:"phase"`
	Waiting *Waiting `json:"waiting,omitempty"`
	// Priority is the train's priority now, its delay band included (see
	// sim.Sim.Priority); 0 before version 4.
	Priority int `json:"priority,omitempty"`
}

// Waiting is a train's queued route request.
type Waiting struct {
	Station   string  `json:"station"`
	Pf        string  `json:"pf"`
	Entry     bool    `json:"entry,omitempty"`
	Since     float64 `json:"since"`
	Reason    string  `json:"reason,omitempty"`
	BlockedBy string  `json:"blockedBy,omitempty"`
	// HeldFor lists the trains precedence holds it back for, comma separated.
	HeldFor string `json:"heldFor,omitempty"`
}

// Held reports whether precedence holds the train back for others.
func (w *Waiting) Held() bool { return w != nil && w.HeldFor != "" }

// Fraction is how far along its track the front is at time t, 0 to 1.
func (m *TrainMsg) Fraction(t float64) float64 {
	if m.TEnd <= m.TStart {
		return 1
	}
	return min(max((t-m.TStart)/(m.TEnd-m.TStart), 0), 1)
}

// EventMsg is one entry of the run log. Fields are the entry's other keys, in
// log order; on the wire they sit flat on the object, as in the JSON log.
type EventMsg struct {
	Header
	Event  string
	Level  string
	Train  string
	Fields []Field
}

type Field struct {
	Key   string
	Value any
}

// DecisionMsg is a precedence decision as it stands at T: a train held back
// for another (Action "hold"), or moved off its timetabled platform
// ("platform"). It is sent again whenever it changes, e.g. when it ends.
type DecisionMsg struct {
	Header
	ID     string  `json:"id"`
	Action string  `json:"action"`
	Kind   string  `json:"kind,omitempty"`
	Train  string  `json:"train"`
	For    string  `json:"for,omitempty"`
	At     string  `json:"at"`
	PfFrom string  `json:"pfFrom,omitempty"`
	PfTo   string  `json:"pfTo,omitempty"`
	Status string  `json:"status"` // active, released, expired, overridden or done
	Source string  `json:"source"` // auto or manual
	Rule   string  `json:"rule,omitempty"`
	Reason string  `json:"reason,omitempty"`
	Gain   float64 `json:"gain,omitempty"` // minutes the other train saves
	Wait   float64 `json:"wait,omitempty"` // minutes the held train is expected to wait
	Since  float64 `json:"since"`
	Until  float64 `json:"until,omitempty"` // when a hold is expected to end
	Ended  float64 `json:"ended,omitempty"`
}

// Active reports whether the decision still stands.
func (m *DecisionMsg) Active() bool { return m.Status == "active" }

type EndMsg struct {
	Header
}

type ErrorMsg struct {
	Header
	Message string `json:"message"`
}

func (*HelloMsg) kind() string    { return TypeHello }
func (*TrackMsg) kind() string    { return TypeTrack }
func (*SwitchMsg) kind() string   { return TypeSwitch }
func (*TrainMsg) kind() string    { return TypeTrain }
func (*EventMsg) kind() string    { return TypeEvent }
func (*DecisionMsg) kind() string { return TypeDecision }
func (*EndMsg) kind() string      { return TypeEnd }
func (*ErrorMsg) kind() string    { return TypeError }

var eventKeys = []string{"type", "t", "event", "level", "train"}

func (e *EventMsg) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	head := []Field{{"type", e.Type}, {"t", e.T}, {"event", e.Event}, {"level", e.Level}, {"train", e.Train}}
	for i, f := range append(head, e.Fields...) {
		if i >= len(head) && slices.Contains(eventKeys, f.Key) {
			continue
		}
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(f.Key)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(f.Value)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON keeps the fields in the order they were written. Numbers are
// kept as json.Number, so they print as written.
func (e *EventMsg) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fmt.Errorf("vizproto: event is not an object")
	}
	*e = EventMsg{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		var v any
		if err := dec.Decode(&v); err != nil {
			return err
		}
		switch key {
		case "type":
			e.Type, _ = v.(string)
		case "t":
			if n, ok := v.(json.Number); ok {
				e.T, _ = n.Float64()
			}
		case "event":
			e.Event, _ = v.(string)
		case "level":
			e.Level, _ = v.(string)
		case "train":
			e.Train, _ = v.(string)
		default:
			e.Fields = append(e.Fields, Field{key, v})
		}
	}
	return nil
}
