package railway

type RailwayEvent string

const (
	// SIM //
	WorldEntered RailwayEvent = "WORLD_ENTER"
	WorldExited  RailwayEvent = "WORLD_EXIT"

	// STN //
	TrainDwellEnd RailwayEvent = "TRAIN_DWELL_END"
	TrainArrived  RailwayEvent = "TRAIN_ARRIVE"
	TrainDeparted RailwayEvent = "TRAIN_DEPART"

	// TRK //
	// TrackReserved  RailwayEvent = "TRACK_RESERVE"
	TrackEntered RailwayEvent = "TRACK_ENTER"
	// TrackOccupied  RailwayEvent = "TRACK_OCCUPY"
	TrackTravelEnd RailwayEvent = "TRACK_TRAVEL_END"
	TrackExited    RailwayEvent = "TRACK_EXITED"

	TrackReleased RailwayEvent = "TRACK_RELEASE"

	PathCompleted RailwayEvent = "PATH_COMPLETED"

	RouteGranted       RailwayEvent = "ROUTE_GRANTED"
	MovementAuthorized RailwayEvent = "MOVEMENT_AUTHORIZED"

	// SWT //
	SwitchSet RailwayEvent = "SWITCH_SET"
)
