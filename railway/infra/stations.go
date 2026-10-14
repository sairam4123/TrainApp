package infra

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"trainapp/railway/ids"
	"trainapp/units"
)

type Station struct {
	Code ids.StnCode
	Name string

	pfNoMapCache map[string]ids.TrackID
	platforms    map[ids.TrackID]*Platform
}

type Platform struct {
	Id ids.TrackID

	PfNo   string
	Length units.Meters

	Track *TrackSegment
}

func (stn *Station) Init() {
	stn.pfNoMapCache = make(map[string]ids.TrackID)
	stn.platforms = make(map[ids.TrackID]*Platform)
}

// NewStationPlatform registers track as platform pfNo. The track must already
// be in the graph. It panics if either the platform number or the track is
// already registered at this station.
func (stn *Station) NewStationPlatform(track *TrackSegment, pfNo string, length units.Meters) {
	if track.Id.IsZero() {
		panic(fmt.Sprintf("infra: station %s platform %s: add the track to the graph before registering it", stn.Code, pfNo))
	}
	if _, ok := stn.pfNoMapCache[pfNo]; ok {
		panic(fmt.Sprintf("infra: station %s already has a platform %s", stn.Code, pfNo))
	}
	if _, ok := stn.platforms[track.Id]; ok {
		panic(fmt.Sprintf("infra: track %s is already a platform of station %s", track, stn.Code))
	}
	pf := &Platform{
		Id:     track.Id,
		PfNo:   pfNo,
		Length: length,
		Track:  track,
	}
	stn.platforms[pf.Id] = pf
	stn.pfNoMapCache[pf.PfNo] = pf.Id
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

// Platforms returns the station's platforms ordered by platform number.
func (stn *Station) Platforms() []*Platform {
	return slices.SortedFunc(maps.Values(stn.platforms), func(a, b *Platform) int {
		return cmp.Or(cmp.Compare(len(a.PfNo), len(b.PfNo)), cmp.Compare(a.PfNo, b.PfNo))
	})
}

func (stn *Station) IsPlatform(trackId ids.TrackID) bool {
	_, ok := stn.platforms[trackId]
	return ok
}
