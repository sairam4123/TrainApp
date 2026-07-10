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

	lockedBy *Train
	isLocked bool
}

func (pt *TrackPoint) WithDeadEnd(isDeadEnd bool) *TrackPoint {
	pt.IsDeadEnd = isDeadEnd
	return pt
}

func (pt *TrackPoint) WithSimLimit(isSimBdary bool) *TrackPoint {
	pt.IsSimBoundary = isSimBdary
	return pt
}

func (ptCtrller *PointController) MoveSwitchState(towardsPointId string) (error, bool) {
	graph := ptCtrller.sim.world.TrackGraph
	neighbors, ok := graph.NeighborMap[ptCtrller.point.Id]

	if !ok {
		fmt.Println("Cannot find switch inside TrackGraph, did you call BuildCacheMap()?")
		return fmt.Errorf("Cannot find switch inside TrackGraph, did you call BuildCacheMap()?"), false
	}

	track, ok := neighbors[towardsPointId]
	if !ok {
		fmt.Println("Cannot find towardsPointId, verify whether the towardsPointId is actually accessible?")
		return fmt.Errorf("Cannot find towardsPointId, verify whether the towardsPointId is actually accessible?"), false
	}
	if _, ok := graph.Edges[track.Id]; !ok {
		fmt.Println("Cannot find the edge, something is terribly wrong!")
		return fmt.Errorf("Cannot find the edge, something is terribly wrong!"), false
	}

	ptCtrller.activeRoute = graph.Edges[track.Id]
	return nil, true
}

func (ptCtrller *PointController) LockPoint(train *Train) bool {
	if ptCtrller.isLocked && ptCtrller.lockedBy.Number == train.Number {
		return true
	}
	if ptCtrller.activeRoute == nil {
		return false
	}
	if ptCtrller.lockedBy != nil && ptCtrller.lockedBy.Number != train.Number {
		fmt.Printf("Point %s is already locked by %s, cannot lock point for %s\n", ptCtrller.point.Id, ptCtrller.lockedBy.GetFullName(), train.GetFullName())
		return false
	}
	fmt.Printf("Locking point %s for %s\n", ptCtrller.point.Id, train.GetFullName())
	ptCtrller.isLocked = true
	ptCtrller.lockedBy = train
	return true
}

func (ptCtrller *PointController) UnlockPoint(train *Train) bool {

	if !ptCtrller.isLocked {
		return true
	}
	if ptCtrller.activeRoute == nil {
		return false
	}
	if ptCtrller.isLocked && ptCtrller.lockedBy.Number != train.Number {
		fmt.Printf("Cannot unlock point %s locked by another train %s for train %s\n", ptCtrller.point.Id, ptCtrller.lockedBy.GetFullName(), train.GetFullName())
		return false
	}
	fmt.Printf("Unlocking point %s by %s\n", ptCtrller.point.Id, train.GetFullName())
	ptCtrller.isLocked = false
	ptCtrller.lockedBy = nil
	return true
}
