package railway

import "fmt"

type TrackPoint struct {
	Id string

	IsDeadEnd     bool
	IsSimBoundary bool
}

type PointController struct {
	sim *Sim

	point *TrackPoint

	activeRoute *GraphEdge

	isLocked bool
}

func (pt *TrackPoint) WithDeadEnd(isDeadEnd bool) *TrackPoint {
	pt.IsDeadEnd = isDeadEnd
	return pt
}

func (pt *TrackPoint) ConfigureSimBoundary(isSimBdary bool) *TrackPoint {
	pt.IsSimBoundary = isSimBdary
	return pt
}

func (ptCtrller *PointController) MoveSwitchState(towardsPointId string) error {
	graph := ptCtrller.sim.world.TrackGraph
	neighbors, ok := graph.NeighborMap[ptCtrller.point.Id]

	if !ok {
		return fmt.Errorf("Cannot find switch inside TrackGraph, did you call BuildCacheMap()?")
	}

	track, ok := neighbors[towardsPointId]
	if !ok {
		return fmt.Errorf("Cannot find towardsPointId, verify whether the towardsPointId is actually accessible?")
	}
	if _, ok := graph.Edges[track.Id]; !ok {
		return fmt.Errorf("Cannot find the edge, something is terribly wrong!")
	}

	ptCtrller.activeRoute = graph.Edges[track.Id]
	return nil
}

func (ptCtrller *PointController) LockPoint() bool {
	if ptCtrller.isLocked {
		return false
	}
	if ptCtrller.activeRoute == nil {
		return false
	}
	ptCtrller.isLocked = true
	return true
}

func (ptCtrller *PointController) UnlockPoint() bool {
	if !ptCtrller.isLocked {
		return false
	}
	if ptCtrller.activeRoute == nil {
		return false
	}
	ptCtrller.isLocked = false
	return true
}
