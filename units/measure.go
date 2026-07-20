package units

type Meters float64
type Minutes float64
type MetersPerMin float64

const MPH_MPM_CONV_FACTOR = 26.8224

func M(length float64) Meters {
	return Meters(length)
}

func KM(length float64) Meters {
	return Meters(length * 1000)
}

func Min(time float64) Minutes {
	return Minutes(time)
}

func Sec(time float64) Minutes {
	return Minutes(time / 60)
}

func Hr(time float64) Minutes {
	return Minutes(time * 60)
}

func KMPH(speed float64) MetersPerMin {
	return MetersPerMin(speed * 1000 / 60)
}

func MilesPH(speed float64) MetersPerMin {
	return MetersPerMin(speed * MPH_MPM_CONV_FACTOR)
}
