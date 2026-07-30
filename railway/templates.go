package railway

import (
	"fmt"
	"strings"
)

// two PF station with Single line
func NewStationClassC(world *World, stnCode, stnName string) (*Station, *TrackPoint, *TrackPoint) {
	stn := world.NewStation(stnCode, stnName)

	stnCodeLwr := strings.ToLower(stnCode)
	sw1Id := fmt.Sprintf("%sSw1", stnCodeLwr)

	stnSw1 := world.NewTrackPoint(sw1Id)

	sw2Id := fmt.Sprintf("%sSw2", stnCodeLwr)

	stnSw2 := world.NewTrackPoint(sw2Id)

	pf1S := fmt.Sprintf("%sPf1S", stnCodeLwr)
	pf1E := fmt.Sprintf("%sPf1E", stnCodeLwr)
	stnPf1S := world.NewTrackPoint(pf1S)
	stnPf1E := world.NewTrackPoint(pf1E)

	pf2S := fmt.Sprintf("%sPf2S", stnCodeLwr)
	pf2E := fmt.Sprintf("%sPf2E", stnCodeLwr)

	stnPf2S := world.NewTrackPoint(pf2S)
	stnPf2E := world.NewTrackPoint(pf2E)

	stnSw1stnPf1S := world.NewSwitchTrack(NewTrackID(stnSw1, stnPf1S))
	stnSw1stnPf2S := world.NewSwitchTrack(NewTrackID(stnSw1, stnPf2S))

	stnSw2stnPf1E := world.NewSwitchTrack(NewTrackID(stnSw2, stnPf1E))
	stnSw2stnPf2E := world.NewSwitchTrack(NewTrackID(stnSw2, stnPf2E))

	stnPf1 := world.NewPlatformTrack(NewTrackID(stnPf1S, stnPf1E))
	stnPf2 := world.NewPlatformTrack(NewTrackID(stnPf2S, stnPf2E))

	stn.NewStationPlatform(stnPf1, "1", 700)
	stn.NewStationPlatform(stnPf2, "2", 700)

	world.TrackGraph.AddTrack(stnSw1, stnPf1S, stnSw1stnPf1S)
	world.TrackGraph.AddTrack(stnSw1, stnPf2S, stnSw1stnPf2S)

	world.TrackGraph.AddTrack(stnSw2, stnPf1E, stnSw2stnPf1E)
	world.TrackGraph.AddTrack(stnSw2, stnPf2E, stnSw2stnPf2E)

	world.TrackGraph.AddTrack(stnPf1S, stnPf1E, stnPf1)
	world.TrackGraph.AddTrack(stnPf2S, stnPf2E, stnPf2)

	return stn, stnSw1, stnSw2
}

func NewTrackID(from, to *TrackPoint) string {
	return fmt.Sprintf("%s%s", from.Id, to.Id)
}

type StationBuilder struct {
	stn *Station

	world *World

	sw1 *TrackPoint
	sw2 *TrackPoint
}

// func (sb *StationBuilder) BuildPlatform(pfLength units.Meters) {

// 	pf1S := fmt.Sprintf("%sPf1S", stnCodeLwr)
// 	pf1E := fmt.Sprintf("%sPf1E", stnCodeLwr)
// 	stnPf1S := world.NewTrackPoint(pf1S)
// 	stnPf1E := world.NewTrackPoint(pf1E)

// 	pf2S := fmt.Sprintf("%sPf2S", stnCodeLwr)
// 	pf2E := fmt.Sprintf("%sPf2E", stnCodeLwr)

// 	stnPf2S := world.NewTrackPoint(pf2S)
// 	stnPf2E := world.NewTrackPoint(pf2E)

// }
