package lab

func kernelVarint(raw []byte, p int) (uint64, int) {
	if p >= len(raw) {
		return 0, -1
	}
	b := raw[p]
	if b < 128 {
		return uint64(b), p + 1
	}
	return kernelSlowVarint(raw, p)
}
func kernelSlowVarint(raw []byte, p int) (uint64, int) {
	var v uint64
	for j := uint(0); j < 10; j++ {
		if p >= len(raw) {
			return 0, -1
		}
		b := raw[p]
		p++
		if j == 9 && b > 1 {
			return 0, -1
		}
		v |= uint64(b&127) << (j * 7)
		if b < 128 {
			return v, p
		}
	}
	return 0, -1
}
func kernelRecord(raw []byte, p int) (uint64, int, int, int) {
	tag, p := kernelVarint(raw, p)
	if p < 0 || tag>>3 == 0 || tag>>3 > 536870911 {
		return 0, 0, 0, -1
	}
	start := p
	switch tag & 7 {
	case 0:
		_, p = kernelVarint(raw, p)
		if p < 0 {
			return 0, 0, 0, -1
		}
	case 1:
		if len(raw)-p < 8 {
			return 0, 0, 0, -1
		}
		p += 8
	case 2:
		n, q := kernelVarint(raw, p)
		if q < 0 || n > uint64(len(raw)-q) {
			return 0, 0, 0, -1
		}
		start = q
		p = q + int(n)
	case 5:
		if len(raw)-p < 4 {
			return 0, 0, 0, -1
		}
		p += 4
	default:
		return 0, 0, 0, -1
	}
	return tag, start, p - start, p
}
