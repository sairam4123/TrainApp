package main

import (
	"fmt"
	"os"
	"trainapp/visualization/vizproto"
)

// call is when a train reached and left a schedule point in a run, and on
// which platform; -1 and "" until it has.
type call struct {
	arr, dep float64
	pf       string
}

// readCalls reads each train's calls from a recording, one per schedule
// point, as trainviz's schedule pane does: a train's k-th TRAIN_ARRIVE is at
// its k-th point, passes included, and the TRAIN_DEPART (or, at the last,
// the SCHEDULE_END) after it is when it left. Nil if the recording does not
// read.
func readCalls(path string) map[string][]call {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	hello, msgs, err := vizproto.ReadAll(f)
	if err != nil {
		return nil
	}
	trackName := map[string]string{}
	for _, t := range hello.World.Tracks {
		trackName[t.ID] = t.Name
	}
	pfNo := map[string]string{} // by track name, as TRAIN_ARRIVE gives it
	for _, stn := range hello.World.Stations {
		for _, pf := range stn.Platforms {
			pfNo[trackName[pf.Track]] = pf.No
		}
	}
	calls := map[string][]call{}
	for _, m := range msgs {
		e, ok := m.(*vizproto.EventMsg)
		if !ok || e.Train == "" {
			continue
		}
		cs := calls[e.Train]
		switch e.Event {
		case "TRAIN_ARRIVE":
			c := call{arr: e.T, dep: -1}
			for _, f := range e.Fields {
				if f.Key == "track" {
					c.pf = pfNo[fmt.Sprint(f.Value)]
				}
			}
			calls[e.Train] = append(cs, c)
		case "TRAIN_DEPART", "SCHEDULE_END":
			if n := len(cs); n > 0 && cs[n-1].dep < 0 {
				cs[n-1].dep = e.T
			}
		}
	}
	return calls
}
