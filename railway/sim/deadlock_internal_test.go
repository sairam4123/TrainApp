package sim

import (
	"fmt"
	"testing"
	"trainapp/railway/ids"
)

func TestFindCycle(t *testing.T) {
	id := func(n string) ids.TrainID { return ids.TrainID{Number: n} }
	link := func(a, b string) WaitLink { return WaitLink{Train: id(a), For: id(b)} }
	graph := func(ls ...WaitLink) map[ids.TrainID][]WaitLink {
		g := map[ids.TrainID][]WaitLink{}
		for _, l := range ls {
			g[l.Train] = append(g[l.Train], l)
		}
		return g
	}
	nodes := []ids.TrainID{id("A"), id("B"), id("C"), id("D")}
	cases := []struct {
		name  string
		edges map[ids.TrainID][]WaitLink
		want  string
	}{
		{"none", graph(link("A", "B"), link("B", "C")), "[]"},
		{"two", graph(link("A", "B"), link("B", "A")), "[A B]"},
		// A waits on the cycle without being in it
		{"behind one", graph(link("A", "B"), link("B", "C"), link("C", "D"), link("D", "B")), "[B C D]"},
		{"dead end first", graph(link("A", "D"), link("A", "B"), link("B", "A")), "[A B]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []ids.TrainID
			for _, l := range findCycle(nodes, c.edges) {
				got = append(got, l.Train)
			}
			if s := fmt.Sprint(got); s != c.want {
				t.Errorf("got %s, want %s", s, c.want)
			}
		})
	}
}
