package jellyfin

import (
	"math"
	"strings"
)

// Jellyfin has no poster palette like Plex's UltraBlurColors, but every
// image tag comes with a BlurHash. Its corners give the same four colours
// (0xRRGGBB): top-left, top-right, bottom-right, bottom-left.

const base83 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~"

func decode83(s string) (int, bool) {
	v := 0
	for i := 0; i < len(s); i++ {
		d := strings.IndexByte(base83, s[i])
		if d < 0 {
			return 0, false
		}
		v = v*83 + d
	}
	return v, true
}

func srgbToLinear(v int) float64 {
	f := float64(v) / 255
	if f <= 0.04045 {
		return f / 12.92
	}
	return math.Pow((f+0.055)/1.055, 2.4)
}

func linearToSrgb(f float64) uint32 {
	f = math.Max(0, math.Min(1, f))
	if f <= 0.0031308 {
		return uint32(f*12.92*255 + 0.5)
	}
	return uint32((1.055*math.Pow(f, 1/2.4)-0.055)*255 + 0.5)
}

// blurCorners decodes a BlurHash at its four corners; all zero when the
// hash does not parse.
func blurCorners(hash string) [4]uint32 {
	var out [4]uint32
	if len(hash) < 6 {
		return out
	}
	flag, ok := decode83(hash[:1])
	if !ok {
		return out
	}
	nx, ny := flag%9+1, flag/9+1
	if len(hash) != 4+2*nx*ny {
		return out
	}
	qmax, ok := decode83(hash[1:2])
	if !ok {
		return out
	}
	maxAC := float64(qmax+1) / 166
	colors := make([][3]float64, nx*ny)
	for i := range colors {
		if i == 0 {
			v, ok := decode83(hash[2:6])
			if !ok {
				return out
			}
			colors[0] = [3]float64{srgbToLinear(v >> 16), srgbToLinear(v >> 8 & 255), srgbToLinear(v & 255)}
			continue
		}
		v, ok := decode83(hash[4+i*2 : 6+i*2])
		if !ok {
			return out
		}
		q := [3]int{v / (19 * 19), v / 19 % 19, v % 19}
		for c := range 3 {
			x := float64(q[c]-9) / 9
			colors[i][c] = math.Copysign(x*x, x) * maxAC
		}
	}
	// corners at x, y in {0, 1}: cos(pi*i*x) is 1 or (-1)^i
	for k, xy := range [4][2]int{{0, 0}, {1, 0}, {1, 1}, {0, 1}} {
		var px [3]float64
		for j := range ny {
			for i := range nx {
				b := 1.0
				if xy[0] == 1 && i%2 == 1 {
					b = -b
				}
				if xy[1] == 1 && j%2 == 1 {
					b = -b
				}
				for c := range 3 {
					px[c] += b * colors[i+j*nx][c]
				}
			}
		}
		out[k] = linearToSrgb(px[0])<<16 | linearToSrgb(px[1])<<8 | linearToSrgb(px[2])
	}
	return out
}
