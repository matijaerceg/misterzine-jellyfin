package ui

import (
	"math"

	"plexcrt/internal/gfx"
)

// patreonAddress is shown in the Options, report and Updates footers.
const patreonAddress = "patreon.com/MisterZine"

// The supporter star after an extra's label in Options: five broad points
// (thin ones smear over composite) in the MisterZine purple, starH lines
// tall and 9/8 as wide for the raster's pixels, drawn over the page's
// background so it is copied, not blended, and made on first use.
const (
	supporterStarH   = 14
	supporterStarGap = 8 // between the label and the star
)

var supporterStarImg *gfx.Image

func supporterStar() *gfx.Image {
	if supporterStarImg != nil {
		return supporterStarImg
	}
	// the corners, point up, in square pixels: top at 0, the lower points
	// on the bottom line
	const inner = 0.5 // of the outer radius; a regular star's is 0.38
	r := float64(supporterStarH) / (1 + math.Cos(math.Pi/5))
	sw := 2 * r * math.Sin(2*math.Pi/5) // its width in square pixels
	var px, py [10]float64
	for i := range px {
		k := 1.0
		if i%2 == 1 {
			k = inner
		}
		ang := -math.Pi/2 + float64(i)*math.Pi/5
		px[i], py[i] = sw/2+k*r*math.Cos(ang), r+k*r*math.Sin(ang)
	}
	inside := func(x, y float64) bool {
		in := false
		for i, j := 0, len(px)-1; i < len(px); j, i = i, i+1 {
			if (py[i] > y) != (py[j] > y) && x < px[j]+(y-py[j])*(px[i]-px[j])/(py[i]-py[j]) {
				in = !in
			}
		}
		return in
	}
	w, h := int(math.Ceil(sw*9/8)), supporterStarH
	im := &gfx.Image{W: w, H: h, Pix: make([]byte, w*h*4)}
	mix := func(bg, fg gfx.Color, shift uint, cov float64) byte {
		b, f := float64(byte(bg>>shift)), float64(byte(fg>>shift))
		return byte(b + (f-b)*cov + 0.5)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					if inside((float64(x)+(float64(sx)+0.5)/4)*8/9, float64(y)+(float64(sy)+0.5)/4) {
						n++
					}
				}
			}
			cov, p := float64(n)/16, (y*w+x)*4
			im.Pix[p], im.Pix[p+1], im.Pix[p+2] = mix(gfx.Bg, gfx.Purple, 0, cov), mix(gfx.Bg, gfx.Purple, 8, cov), mix(gfx.Bg, gfx.Purple, 16, cov)
			im.Pix[p+3] = 255
		}
	}
	supporterStarImg = im
	return im
}

// PremiumSettings is Config.Premium for the host: the extras' own settings,
