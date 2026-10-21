package jellyfin

import "strings"

// Text helpers carried over unchanged from the plex package: the UI folds
// every server string to the glyph atlases' ASCII.

// Fold maps text to the printable ASCII the glyph atlases hold: dashes,
// quotes and ellipses to their plain forms, Latin letters (Latin-1 and
// Latin Extended-A) to their base letter, anything else to a space.
func Fold(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7E {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 0x20 && r <= 0x7E:
			b.WriteRune(r)
		case r == '\t' || r == '\n' || r == 0xA0:
			b.WriteByte(' ')
		case r == 0x2013 || r == 0x2014 || r == 0x2212 || r == 0x2010 || r == 0x2011:
			b.WriteByte('-')
		case r == 0x2018 || r == 0x2019 || r == 0x2032 || r == 0x201A:
			b.WriteByte('\'')
		case r == 0x201C || r == 0x201D || r == 0x201E || r == 0x2033:
			b.WriteByte('"')
		case r == 0x2026:
			b.WriteString("...")
		case r == 0x2022 || r == 0xB7:
			b.WriteByte('-')
		case r >= 0xC0 && r <= 0xFF:
			b.WriteByte(latin1[r-0xC0])
		case r == 0x152:
			b.WriteString("OE")
		case r == 0x153:
			b.WriteString("oe")
		case r == 0x132:
			b.WriteString("IJ")
		case r == 0x133:
			b.WriteString("ij")
		case r >= 0x100 && r <= 0x17F:
			b.WriteByte(latinA[r-0x100])
		case r >= 0x218 && r <= 0x21B: // Romanian S and T with a comma
			b.WriteByte("SsTt"[r-0x218])
		default:
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// latin1 holds the base letters of U+00C0..U+00FF.
var latin1 = []byte("AAAAAAACEEEEIIIIDNOOOOOxOUUUUYTsaaaaaaaceeeeiiiidnooooo/ouuuuyty")

// latinA holds the base letters of U+0100..U+017F (Latin Extended-A: the
// Central European, Baltic and Turkish letters), a row of 16 a line.
var latinA = []byte("AaAaAaCcCcCcCcDd" + "DdEeEeEeEeEeGgGg" + "GgGgHhHhIiIiIiIi" + "IiIiJjKkkLlLlLlL" +
	"lLlNnNnNnnNnOoOo" + "OoOoRrRrRrSsSsSs" + "SsTtTtTtUuUuUuUu" + "UuUuWwYyYZzZzZzs")

// langNames are English names for language tags whose native names are
// written in scripts the fonts do not cover.
var langNames = map[string]string{
	"ar": "Arabic", "el": "Greek", "he": "Hebrew", "iw": "Hebrew", "ja": "Japanese", "ko": "Korean",
	"zh": "Chinese", "yue": "Cantonese", "ru": "Russian", "uk": "Ukrainian", "bg": "Bulgarian",
	"sr": "Serbian", "mk": "Macedonian", "be": "Belarusian", "kk": "Kazakh", "th": "Thai",
	"hi": "Hindi", "bn": "Bengali", "ta": "Tamil", "te": "Telugu", "ml": "Malayalam", "kn": "Kannada",
	"mr": "Marathi", "gu": "Gujarati", "pa": "Punjabi", "ur": "Urdu", "fa": "Persian", "ps": "Pashto",
	"ka": "Georgian", "hy": "Armenian", "am": "Amharic", "km": "Khmer", "lo": "Lao", "my": "Burmese",
	"si": "Sinhala", "ne": "Nepali", "mn": "Mongolian", "bo": "Tibetan", "vi": "Vietnamese",
	"ms": "Malay", "id": "Indonesian", "tl": "Filipino", "fil": "Filipino", "sw": "Swahili",
	"tr": "Turkish", "az": "Azerbaijani", "uz": "Uzbek", "ky": "Kyrgyz", "tg": "Tajik",
}

// LangName is the English name of a language tag ("ar", "zh-Hant"), or
// "Language xx" when unknown; "" for no tag.
func LangName(tag string) string {
	if tag == "" {
		return ""
	}
	base := strings.ToLower(strings.SplitN(tag, "-", 2)[0])
	if two, ok := iso639_2[base]; ok {
		base = two // Jellyfin reports three-letter codes ("ara", "zho")
	}
	if n, ok := langNames[base]; ok {
		return n
	}
	return "Language " + strings.ToUpper(base)
}

// iso639_2 maps the three-letter codes Jellyfin reports to the two-letter
// keys of langNames, for the languages langNames names.
var iso639_2 = map[string]string{
	"ara": "ar", "ell": "el", "gre": "el", "heb": "he", "jpn": "ja", "kor": "ko", "zho": "zh", "chi": "zh",
	"rus": "ru", "ukr": "uk", "bul": "bg", "srp": "sr", "mkd": "mk", "mac": "mk", "bel": "be", "kaz": "kk",
	"tha": "th", "hin": "hi", "ben": "bn", "tam": "ta", "tel": "te", "mal": "ml", "kan": "kn", "mar": "mr",
	"guj": "gu", "pan": "pa", "urd": "ur", "fas": "fa", "per": "fa", "pus": "ps", "kat": "ka", "geo": "ka",
	"hye": "hy", "arm": "hy", "amh": "am", "khm": "km", "lao": "lo", "mya": "my", "bur": "my", "sin": "si",
	"nep": "ne", "mon": "mn", "bod": "bo", "tib": "bo", "vie": "vi", "msa": "ms", "may": "ms", "ind": "id",
	"tgl": "tl", "swa": "sw", "tur": "tr", "aze": "az", "uzb": "uz", "kir": "ky", "tgk": "tg",
}
