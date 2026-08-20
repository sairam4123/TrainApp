package railway

import (
	"fmt"
	"strings"
)

// two PF station with Single line
func NewStationSL2PF(world *World, stnCode, stnName string) (*Station, *TrackPoint, *TrackPoint) {
	stn := world.NewStation(stnCode, stnName)

	stnCodeLwr := strings.ToLower(stnCode)
	bp0Id := fmt.Sprintf("%sBp0", stnCodeLwr)

	stnBp0 := world.NewTrackPoint(bp0Id)

	bp1Id := fmt.Sprintf("%sBp1", stnCodeLwr)

	stnBp1 := world.NewTrackPoint(bp1Id)

	sw1AId := fmt.Sprintf("%sSw1A", stnCodeLwr)

	sw2AId := fmt.Sprintf("%sSw2A", stnCodeLwr)

	pf1S := fmt.Sprintf("%sPf1S", stnCodeLwr)
	pf1E := fmt.Sprintf("%sPf1E", stnCodeLwr)
	stnPf1S := world.NewTrackPoint(pf1S)
	stnPf1E := world.NewTrackPoint(pf1E)

	pf2S := fmt.Sprintf("%sPf2S", stnCodeLwr)
	pf2E := fmt.Sprintf("%sPf2E", stnCodeLwr)

	stnPf2S := world.NewTrackPoint(pf2S)
	stnPf2E := world.NewTrackPoint(pf2E)

	NewSwitch(world, sw1AId, stnBp0, stnPf1S, stnPf2S)
	NewSwitch(world, sw2AId, stnBp1, stnPf1E, stnPf2E)
	// stnSw1stnPf1S := world.NewSwitchTrack(NewTrackID(stnBp0, stnPf1S))
	// stnSw1stnPf2S := world.NewSwitchTrack(NewTrackID(stnBp0, stnPf2S))

	// stnSw2stnPf1E := world.NewSwitchTrack(NewTrackID(stnBp1, stnPf1E))
	// stnSw2stnPf2E := world.NewSwitchTrack(NewTrackID(stnBp1, stnPf2E))

	stnPf1 := world.NewPlatformTrack(NewTrackID(stnPf1S, stnPf1E))
	stnPf2 := world.NewPlatformTrack(NewTrackID(stnPf2S, stnPf2E))

	stn.NewStationPlatform(stnPf1, "1", 700)
	stn.NewStationPlatform(stnPf2, "2", 700)

	// world.TrackGraph.AddTrack(stnBp0, stnPf1S, stnSw1stnPf1S)
	// world.TrackGraph.AddTrack(stnBp0, stnPf2S, stnSw1stnPf2S)

	// world.TrackGraph.AddTrack(stnBp1, stnPf1E, stnSw2stnPf1E)
	// world.TrackGraph.AddTrack(stnBp1, stnPf2E, stnSw2stnPf2E)

	world.TrackGraph.AddTrack(stnPf1S, stnPf1E, stnPf1)
	world.TrackGraph.AddTrack(stnPf2S, stnPf2E, stnPf2)

	return stn, stnBp0, stnBp1
}

func NewStationSL3PF(world *World, stnCode, stnName string) (*Station, *TrackPoint, *TrackPoint) {
	stn := world.NewStation(stnCode, stnName)

	stnCodeLwr := strings.ToLower(stnCode)
	bp0Id := fmt.Sprintf("%sBp0", stnCodeLwr)

	stnBp0 := world.NewTrackPoint(bp0Id)

	bp1Id := fmt.Sprintf("%sBp1", stnCodeLwr)

	stnBp1 := world.NewTrackPoint(bp1Id)

	sw1AId := fmt.Sprintf("%sSw1A", stnCodeLwr)

	sw2AId := fmt.Sprintf("%sSw2A", stnCodeLwr)

	pf1S := fmt.Sprintf("%sPf1S", stnCodeLwr)
	pf1E := fmt.Sprintf("%sPf1E", stnCodeLwr)
	stnPf1S := world.NewTrackPoint(pf1S)
	stnPf1E := world.NewTrackPoint(pf1E)

	pf2S := fmt.Sprintf("%sPf2S", stnCodeLwr)
	pf2E := fmt.Sprintf("%sPf2E", stnCodeLwr)

	stnPf2S := world.NewTrackPoint(pf2S)
	stnPf2E := world.NewTrackPoint(pf2E)

	NewSwitch(world, sw1AId, stnBp0, stnPf1S, stnPf2S)
	NewSwitch(world, sw2AId, stnBp1, stnPf1E, stnPf2E)
	// stnSw1stnPf1S := world.NewSwitchTrack(NewTrackID(stnBp0, stnPf1S))
	// stnSw1stnPf2S := world.NewSwitchTrack(NewTrackID(stnBp0, stnPf2S))

	// stnSw2stnPf1E := world.NewSwitchTrack(NewTrackID(stnBp1, stnPf1E))
	// stnSw2stnPf2E := world.NewSwitchTrack(NewTrackID(stnBp1, stnPf2E))

	stnPf1 := world.NewPlatformTrack(NewTrackID(stnPf1S, stnPf1E))
	stnPf2 := world.NewPlatformTrack(NewTrackID(stnPf2S, stnPf2E))

	stn.NewStationPlatform(stnPf1, "1", 700)
	stn.NewStationPlatform(stnPf2, "2", 700)

	// world.TrackGraph.AddTrack(stnBp0, stnPf1S, stnSw1stnPf1S)
	// world.TrackGraph.AddTrack(stnBp0, stnPf2S, stnSw1stnPf2S)

	// world.TrackGraph.AddTrack(stnBp1, stnPf1E, stnSw2stnPf1E)
	// world.TrackGraph.AddTrack(stnBp1, stnPf2E, stnSw2stnPf2E)

	world.TrackGraph.AddTrack(stnPf1S, stnPf1E, stnPf1)
	world.TrackGraph.AddTrack(stnPf2S, stnPf2E, stnPf2)

	return stn, stnBp0, stnBp1
}

func NewTrackID(from, to *TrackPoint) string {
	if from.Id > to.Id {
		from, to = to, from
	}
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
