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
	TrackEntered RailwayEvent = "TRACK_ENTER" // the front enters a track
	// TrackOccupied  RailwayEvent = "TRACK_OCCUPY"
	TrackTravelEnd RailwayEvent = "TRACK_TRAVEL_END" // the front reaches a track's end
	// TrackExited is the rear leaving a track behind the front's, while the
	// front runs; it is freed (TRACK_RELEASE) at once.
	TrackExited RailwayEvent = "TRACK_EXITED"
	// TrackApproach is a running train reaching its approach point, where it
	// asks for the section after its authority (see sim/controller.go).
	TrackApproach RailwayEvent = "TRACK_APPROACH"

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
