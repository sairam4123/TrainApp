package units

import "math"

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

// InKMPH is the speed in km/h, rounded to a clean number when that converts
// back to exactly the same speed, so it reads 110 rather than
// 110.00000000000001.
func (v MetersPerMin) InKMPH() float64 {
	raw := float64(v) * 60 / 1000
	if r := math.Round(raw*1e6) / 1e6; KMPH(r) == v {
		return r
	}
	return raw
}

func MilesPH(speed float64) MetersPerMin {
	return MetersPerMin(speed * MPH_MPM_CONV_FACTOR)
}
