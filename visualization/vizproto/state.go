package vizproto

import (
	"cmp"
	"maps"
	"slices"
)

// State is the railway as a stream describes it up to some message.
type State struct {
	T         float64
	Tracks    map[string]TrackMsg // only tracks that are held
	Switches  map[string]SwitchMsg
	Trains    map[string]TrainMsg
	Decisions map[string]DecisionMsg // every decision so far, by id
}

func NewState() *State {
	return &State{
		Tracks:    map[string]TrackMsg{},
		Switches:  map[string]SwitchMsg{},
		Trains:    map[string]TrainMsg{},
		Decisions: map[string]DecisionMsg{},
	}
}

func (s *State) Apply(m Message) {
	s.T = m.Head().T
	switch m := m.(type) {
	case *TrackMsg:
		if m.ReservedBy == "" && m.OccupiedBy == "" {
			delete(s.Tracks, m.Track)
		} else {
			s.Tracks[m.Track] = *m
		}
	case *SwitchMsg:
		s.Switches[m.Block] = *m
	case *TrainMsg:
		s.Trains[m.Train] = *m
	case *DecisionMsg:
		s.Decisions[m.ID] = *m
	}
}

// ActiveDecisions returns the decisions that still stand for the train,
// oldest first; every train's if train is empty.
func (s *State) ActiveDecisions(train string) []DecisionMsg {
	var out []DecisionMsg
	for _, d := range s.Decisions {
		if d.Active() && (train == "" || d.Train == train) {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b DecisionMsg) int {
		return cmp.Or(cmp.Compare(a.Since, b.Since), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// TrackHolder is who has reserved and who has occupied the track.
func (s *State) TrackHolder(track string) (reservedBy, occupiedBy string) {
	h := s.Tracks[track]
	return h.ReservedBy, h.OccupiedBy
}

func (s *State) Clone() *State {
	return &State{T: s.T, Tracks: maps.Clone(s.Tracks), Switches: maps.Clone(s.Switches), Trains: maps.Clone(s.Trains), Decisions: maps.Clone(s.Decisions)}
}
