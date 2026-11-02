package lyrics

import (
	"encoding/xml"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// A Line is a line of timed lyrics, with its translation if any, and
// when each of its words is sung.
type Line struct {
	At          time.Duration
	End         time.Duration
	Text        string
	Translation string
	Words       []Word
	// Timed says the words carry their own times, as QQ Music's word-timed
	// lyrics give them; else they share the line's time evenly.
	Timed bool
}

// A Word is a word of a line, or a character of Chinese or Japanese,
// with when it is sung.
type Word struct {
	Text       string
	Start, End time.Duration
}

var lrcTime = regexp.MustCompile(`\[(\d+):(\d+)(?:[.:](\d+))?\]`)

// parseLRC reads lyrics in the LRC format, as QQ Music serves those it has
// no word times for: a line may carry several times, and credits come as
// lines of their own.
func parseLRC(lrc string) []Line {
	var out []Line
	for _, raw := range strings.Split(lrc, "\n") {
		times := lrcTime.FindAllStringSubmatchIndex(raw, -1)
		if len(times) == 0 {
			continue
		}
		text := strings.TrimSpace(raw[times[len(times)-1][1]:])
		for _, m := range times {
			min, _ := strconv.Atoi(raw[m[2]:m[3]])
			sec, _ := strconv.Atoi(raw[m[4]:m[5]])
			var frac time.Duration
			if m[6] >= 0 {
				f := raw[m[6]:m[7]]
				n, _ := strconv.Atoi(f)
				frac = time.Duration(n) * time.Second
				for range len(f) {
					frac /= 10
				}
			}
			at := time.Duration(min)*time.Minute + time.Duration(sec)*time.Second + frac
			out = append(out, Line{At: at, Text: text})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

// parseQRC reads QQ Music's word-timed lyrics: a line is
// "[start,length]word(start,length)word(start,length)…", in milliseconds,
// each word before its time. A word's own text may hold brackets: those
// not followed by a time are its text.
func parseQRC(qrc string) []Line {
	var out []Line
	for _, raw := range strings.Split(qrc, "\n") {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		m := qrcLine.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		l := Line{At: millis(m[1]), Timed: true}
		l.End = l.At + millis(m[2])
		rest := raw[len(m[0]):]
		text := ""
		for rest != "" {
			w := qrcWord.FindStringSubmatchIndex(rest)
			if w == nil {
				text += rest
				break
			}
			text += rest[:w[0]]
			start := millis(rest[w[2]:w[3]])
			word := Word{Text: text, Start: start, End: start + millis(rest[w[4]:w[5]])}
			text, rest = "", rest[w[1]:]
			if strings.TrimSpace(word.Text) == "" {
				// A space between words: it belongs to the word before.
				if n := len(l.Words); n > 0 && word.Text != "" {
					l.Words[n-1].Text += " "
				}
				continue
			}
			l.Words = append(l.Words, word)
		}
		if text != "" && len(l.Words) > 0 {
			l.Words[len(l.Words)-1].Text += text
		}
		for _, w := range l.Words {
			l.Text += w.Text
		}
		l.Text = strings.TrimSpace(l.Text)
		if l.Text != "" {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out
}

var (
	qrcLine = regexp.MustCompile(`^\[(\d+),(\d+)\]`)
	qrcWord = regexp.MustCompile(`\((\d+),(\d+)(?:,\d+)?\)`)
)

func millis(s string) time.Duration {
	n, _ := strconv.Atoi(s)
	return time.Duration(n) * time.Millisecond
}

// qrcContent takes the lyrics out of the XML QQ Music wraps its word-timed
// lyrics in, the LyricContent of <Lyric_1 …/>: written as XML escapes it,
// or not, as some are, with quotes of their own inside.
func qrcContent(s string) string {
	if !strings.HasPrefix(strings.TrimSpace(s), "<") {
		return s
	}
	d := xml.NewDecoder(strings.NewReader(s))
	d.Strict = false
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		if el, ok := tok.(xml.StartElement); ok {
			for _, a := range el.Attr {
				if a.Name.Local == "LyricContent" {
					// A quote inside cuts the strict reading short: then
					// it takes all up to the quote that ends the element.
					if loose := looseContent(s); len(loose) > len(a.Value) {
						return loose
					}
					return a.Value
				}
			}
		}
	}
	return looseContent(s)
}

// looseContent reads LyricContent as written, up to the quote that its
// element's end follows.
func looseContent(s string) string {
	_, rest, ok := strings.Cut(s, `LyricContent="`)
	if !ok {
		return ""
	}
	end := strings.LastIndex(rest, `"/>`)
	if end < 0 {
		end = strings.LastIndex(rest, `">`)
	}
	if end < 0 {
		return ""
	}
	return html.UnescapeString(rest[:end])
}

// parseTimed reads lyrics as QQ Music serves them, word-timed or by lines.
func parseTimed(s string) []Line {
	s = qrcContent(s)
	if lines := parseQRC(s); len(lines) > 0 {
		return lines
	}
	var lines []Line
	for _, l := range parseLRC(s) {
		// QQ Music marks a line it has no translation of with "//".
		if l.Text != "" && l.Text != "//" {
			lines = append(lines, l)
		}
	}
	return lines
}

// Build makes the lines to show from what QQ Music serves: lyrics,
// word-timed or timed by lines, with the translation of each.
func Build(lyric, translation string) []Line {
	lines := parseTimed(lyric)
	if len(lines) > 0 && !lines[0].Timed {
		for i := range lines {
			end := lines[i].At + 6*time.Second
			if i+1 < len(lines) {
				end = min(end, lines[i+1].At)
			}
			lines[i].End = end
			lines[i].Words = SpreadWords(lines[i].Text, lines[i].At, end)
		}
	}
	// Translations go to the line nearest their time: word-timed lines
	// start a little off the timed lines the translation follows.
	for _, t := range parseTimed(translation) {
		best, bestGap := -1, 1500*time.Millisecond
		for i := range lines {
			gap := lines[i].At - t.At
			if gap < 0 {
				gap = -gap
			}
			if gap < bestGap {
				best, bestGap = i, gap
			}
		}
		if best >= 0 && lines[best].Translation == "" {
			lines[best].Translation = t.Text
		}
	}
	return lines
}

// mergeLyrics pairs timed lines with their translations, without words'
// own times.
func mergeLyrics(lrc, translation string) []Line {
	return Build(lrc, translation)
}

// SpreadWords splits text into words, each Chinese, Japanese or Korean
// character one of them, and shares the time from start to end among them
// by their lengths, for lines whose words have no times of their own.
func SpreadWords(text string, start, end time.Duration) []Word {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			if cur.Len() > 0 {
				cur.WriteRune(' ')
				flush()
			}
		case isCJK(r):
			flush()
			words = append(words, string(r))
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	if len(words) == 0 {
		return nil
	}
	total := 0
	for _, w := range words {
		total += utf8.RuneCountInString(strings.TrimSpace(w))
	}
	// Singing takes most of the line, not the pause before the next.
	span := time.Duration(float64(end-start) * 0.85)
	out := make([]Word, len(words))
	at := start
	for i, w := range words {
		d := span * time.Duration(utf8.RuneCountInString(strings.TrimSpace(w))) / time.Duration(max(total, 1))
		out[i] = Word{Text: w, Start: at, End: at + d}
		at += d
	}
	return out
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// Index returns the line sung at pos, or -1 before the first.
func Index(lines []Line, pos time.Duration) int {
	i := sort.Search(len(lines), func(i int) bool { return lines[i].At > pos })
	return i - 1
}
