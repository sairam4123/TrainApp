package railway

type BlockSection struct {
	stnA *Station
	stnB *Station
	Id   string

	tracks []*TrackSegment
}

func (bsec *BlockSection) Init(stnA *Station, stnB *Station) {
	bsec.tracks = make([]*TrackSegment, 0)
	bsec.stnA = stnA
	bsec.stnB = stnB
}

func (bsec *BlockSection) AddTrack(td *TrackSegment) {
	if bsec.stnA == nil || bsec.stnB == nil {
		panic("Station A or Station B is nil, did u forget to intialize?")
	}
	bsec.tracks = append(bsec.tracks, td)

}
