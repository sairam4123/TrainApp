package main

// The KPI pages in the viewer: drawing them across the screen and the
// keys they share.

// pageBody draws the delay page or the occupancy tables across the screen
// above the key line.
func (m *model) pageBody() []string {
	h := max(m.height-m.headerLines()-1, 3)
	s := m.st
	if s.rep == nil {
		title := map[page]string{pageDelay: "delay", pageOccupancy: "occupancy"}[m.page]
		return box(title, []string{"", " " + s.missing}, m.width, h)
	}
	if m.page == pageDelay {
		return s.delayPage(m.width, h, m.rep)
	}
	return s.occTablePage(m.width, h)
}

// scrollPage moves the focused table's selection by d rows.
func (m *model) scrollPage(d int) {
	s := m.st
	if s.rep == nil {
		return
	}
	if m.page == pageDelay {
		n := map[int]int{dTrains: len(s.delayTrains()), dStations: len(s.rep.StationDelays), dCalls: len(s.stationCalls()), dPlaces: len(waitPlaces(s.rep))}[s.dFocus]
		s.dSel[s.dFocus] = min(max(s.dSel[s.dFocus]+d, 0), max(n-1, 0))
		if s.dFocus == dStations {
			s.dSel[dCalls], s.dOff[dCalls] = 0, 0
		}
		return
	}
	f := s.oFocus
	s.oSel[f] = min(max(s.oSel[f]+d, 0), max(s.occRows(f)-1, 0))
	if f == tableStations {
		s.oSel[tablePlatforms], s.oOff[tablePlatforms] = 0, 0
	}
}

// pageRows is how far pgup and pgdn move.
func (m *model) pageRows() int { return max(m.height/3, 1) }

// kpiKey handles the delay and occupancy pages' own keys, and reports
// whether it did.
func (m *model) kpiKey(key string) bool {
	s := m.st
	if s.rep == nil {
		return false
	}
	switch {
	case m.page == pageDelay:
		return m.delayKey(key)
	case s.table:
		return m.occTableKey(key)
	}
	switch key {
	case "m":
		s.metric = (s.metric + 1) % len(metrics)
	case "v":
		s.table = true
		s.focus = ""
	case "esc":
		s.focus = ""
	default:
		return false
	}
	m.redraw()
	return true
}

// tableKey handles moving through the focused table, and reports whether
// it did.
func (m *model) tableKey(key string) bool {
	switch key {
	case "up", "k":
		m.scrollPage(-1)
	case "down", "j":
		m.scrollPage(1)
	case "pgdown", "space":
		m.scrollPage(m.pageRows())
	case "pgup":
		m.scrollPage(-m.pageRows())
	case "home":
		m.scrollPage(-1 << 20)
	case "end":
		m.scrollPage(1 << 20)
	default:
		return false
	}
	return true
}
