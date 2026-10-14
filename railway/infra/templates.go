package infra

import "trainapp/railway/ids"

// two PF station with Single line
func NewStationSL2PF(world *World, stnCode ids.StnCode, stnName string) (*Station, *TrackPoint, *TrackPoint) {
	stn := world.NewStation(stnCode, stnName)

	stnBp0 := world.NewTrackPoint(ids.PointID{StnCode: stnCode, Type: "Bp", Seq: "0"})
	stnBp1 := world.NewTrackPoint(ids.PointID{StnCode: stnCode, Type: "Bp", Seq: "1"})

	stnPf1S := world.NewTrackPoint(ids.PointID{StnCode: stnCode, Type: "Pf", Seq: "1", Other: "S"})
	stnPf1E := world.NewTrackPoint(ids.PointID{StnCode: stnCode, Type: "Pf", Seq: "1", Other: "E"})

	stnPf2S := world.NewTrackPoint(ids.PointID{StnCode: stnCode, Type: "Pf", Seq: "2", Other: "S"})
	stnPf2E := world.NewTrackPoint(ids.PointID{StnCode: stnCode, Type: "Pf", Seq: "2", Other: "E"})

	NewSwitch(world, ids.PointID{StnCode: stnCode, Type: "Sw", Seq: "1", Other: "A"}.String(), stnBp0, stnPf1S, stnPf2S)
	NewSwitch(world, ids.PointID{StnCode: stnCode, Type: "Sw", Seq: "2", Other: "A"}.String(), stnBp1, stnPf1E, stnPf2E)

	stnPf1 := world.TrackGraph.AddTrack(stnPf1S, stnPf1E, world.NewPlatformTrack("")).Track
	stnPf2 := world.TrackGraph.AddTrack(stnPf2S, stnPf2E, world.NewPlatformTrack("")).Track

	stn.NewStationPlatform(stnPf1, "1", 700)
	stn.NewStationPlatform(stnPf2, "2", 700)

	return stn, stnBp0, stnBp1
}
