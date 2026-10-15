package ops

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

	RouteGranted              RailwayEvent = "ROUTE_GRANTED"
	MovementAuthorized        RailwayEvent = "MOVEMENT_AUTHORIZED"
	MovementAuthorityExtended RailwayEvent = "MOVEMENT_AUTHORITY_EXTENDED"
	MovementAuthorityEnded    RailwayEvent = "MOVEMENT_AUTHORITY_END"

	ScheduleEnd RailwayEvent = "SCHEDULE_END"

	PriorityChanged RailwayEvent = "PRIORITY_CHANGE"

	// PrecedenceReview asks the dispatcher to look again at a train it holds
	// back for another; PrecedenceOverride applies an operator's change to
	// the precedence decisions.
	PrecedenceReview   RailwayEvent = "PRECEDENCE_REVIEW"
	PrecedenceOverride RailwayEvent = "PRECEDENCE_OVERRIDE"

	// SWT //
	SwitchSet RailwayEvent = "SWITCH_SET"
)
