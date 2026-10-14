// Package ids holds the typed identifiers shared across the railway packages.
// They are comparable structs, so they work as map keys, and distinct types,
// so a point id can never be used where a track id is expected.
package ids

import (
	"cmp"
	"strings"
)

// StnCode is a station code, e.g. "TPJ".
type StnCode string

func (c StnCode) String() string { return string(c) }

// PointID identifies a track point, e.g. {TPJ, Pf, 1, S} for the start of
// platform 1 at TPJ ("tpjPf1S").
type PointID struct {
	StnCode StnCode
	Type    string
	Seq     string
	Other   string
}

func (p PointID) String() string {
	return strings.ToLower(string(p.StnCode)) + p.Type + p.Seq + p.Other
}

func (p PointID) IsZero() bool { return p == PointID{} }

func (p PointID) Compare(o PointID) int {
	return cmp.Or(
		cmp.Compare(p.StnCode, o.StnCode),
		cmp.Compare(p.Type, o.Type),
		cmp.Compare(p.Seq, o.Seq),
		cmp.Compare(p.Other, o.Other),
	)
}

// TrackID identifies a track by the two points it joins. It is unordered:
// NewTrackID(a, b) == NewTrackID(b, a).
type TrackID struct {
	Point1 PointID
	Point2 PointID
}

func NewTrackID(a, b PointID) TrackID {
	if a.Compare(b) > 0 {
		a, b = b, a
	}
	return TrackID{Point1: a, Point2: b}
}

func (t TrackID) String() string {
	return t.Point1.String() + "-" + t.Point2.String()
}

func (t TrackID) IsZero() bool { return t == TrackID{} }

func (t TrackID) Compare(o TrackID) int {
	return cmp.Or(t.Point1.Compare(o.Point1), t.Point2.Compare(o.Point2))
}

// TrainID identifies a train by its number.
type TrainID struct {
	Number string
}

func (t TrainID) String() string { return t.Number }

func (t TrainID) IsZero() bool { return t == TrainID{} }
