package railway

import (
	"trainapp/units"
)

type Station struct {
	Code string
	Name string

	pfNoMapCache map[string]string
	platforms    map[string]*Platform
}

type Platform struct {
	Id string

	PfNo   string
	Length units.Meters

	Track *TrackSegment
}

func (stn *Station) Init() {
	stn.pfNoMapCache = make(map[string]string)
	stn.platforms = make(map[string]*Platform)
}

func (stn *Station) NewStationPlatform(track *TrackSegment, pfNo string, length units.Meters) {
	pf := &Platform{
		Id:     track.Id,
		PfNo:   pfNo,
		Length: length,
		Track:  track,
	}
	stn.platforms[pf.Id] = pf
	stn.pfNoMapCache[pf.PfNo] = pf.Id
	// stn.Platforms = append(stn.Platforms, pf)
}

func (stn *Station) FindAvailableStnPlatform(prefPfNo string) *TrackSegment {
	// for _, pf := range stn.Platforms {
	// 	if pf.PfNo == prefPfNo && pf.Track.IsAvailable() {
	// 		return pf.Track
	// 	}
	// }
	pfTrackId, ok := stn.pfNoMapCache[prefPfNo]
	if !ok {
		return nil
	}
	pf, ok := stn.platforms[pfTrackId]
	if ok && pf.Track.IsAvailable() {
		return pf.Track
	}

	// find the first Available track
	for _, pf := range stn.platforms {
		if pf.Track.IsAvailable() {
			return pf.Track
		}
	}

	// we don't have any platform atp -- just route to the first platform for the time being
	// for _, pf := range stn.Platforms {
	// 	return pf.Track
	// }
	return nil
}

func (stn *Station) FindStnPlatform(pfNo string) *TrackSegment {
	pfTrackId, ok := stn.pfNoMapCache[pfNo]
	if !ok {
		return nil
	}
	pf, ok := stn.platforms[pfTrackId]
	if ok {
		return pf.Track
	}
	return nil
}

func (stn *Station) IsPlatform(trackId string) bool {
	_, ok := stn.platforms[trackId]
	return ok
}
