package railway

import (
	"fmt"
	"trainapp/units"
)

type Station struct {
	Code string
	Name string

	Platforms []*Platform
}

type Platform struct {
	Id string

	PfNo   string
	Length units.Meters

	Track *TrackSegment
}

func (stn *Station) Init() {
	stn.Platforms = make([]*Platform, 0)
}

func (stn *Station) AddPlatform(pfData *Platform) {
	if pfData.Track == nil {
		fmt.Printf("[WARN] pfData.track is nil, did u pass the track?")
	}
	if pfData.Id == "" {
		pfData.Id = pfData.Track.Id
	}
	stn.Platforms = append(stn.Platforms, pfData)
}

func (stn *Station) NewStationPlatform(track *TrackSegment, pfNo string, length units.Meters) {
	pf := &Platform{
		Id:     track.Id,
		PfNo:   pfNo,
		Length: length,
		Track:  track,
	}
	stn.Platforms = append(stn.Platforms, pf)
}

func (stn *Station) FindAvailableStnPlatform(prefPfNo string) *TrackSegment {
	for _, pf := range stn.Platforms {
		if pf.PfNo == prefPfNo && pf.Track.IsAvailable() {
			return pf.Track
		}
	}

	// find the first Available track
	for _, pf := range stn.Platforms {
		if pf.Track.IsAvailable() {
			return pf.Track
		}
	}

	// just route to the random platform for the time being
	for _, pf := range stn.Platforms {
		return pf.Track
	}
	return nil
}
