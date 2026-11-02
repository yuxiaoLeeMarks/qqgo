package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"qqgo/internal/qqmusic"
)

// Radios play on without end: QQ Music's personal radio, 猜你喜欢, as
// "guess", and 专属雷达, the songs its radar finds from what the listener
// saved, as "radar".

var fmModes = []struct{ mode, label string }{
	{"guess", "猜你喜欢"},
	{"radar", "专属雷达"},
}

// radioLabel names a radio as the listener knows it.
func radioLabel(name string) string {
	for _, m := range fmModes {
		if name == m.mode {
			return "个性电台 · " + m.label
		}
	}
	return "个性电台"
}

func playable(tracks []qqmusic.Track) []qqmusic.Track {
	var out []qqmusic.Track
	for _, t := range tracks {
		if t.Playable() {
			out = append(out, t)
		}
	}
	return out
}

// likedPlaylist returns the ID of the listener's liked songs, or 0.
func (a *app) likedPlaylist() int64 {
	for _, pl := range a.mine {
		if pl.SpecialType == 5 {
			return pl.ID
		}
	}
	return 0
}

// These give the radios' songs; tests replace them.
var (
	guessTracks = qqmusic.GuessTracks
	radarTracks = qqmusic.RadarTracks
)

// radioFeed returns what gives the radio name more tracks: it loads them
// on a goroutine, and gives them to got on the main thread, none if it
// cannot. The radar goes on page by page.
func (a *app) radioFeed(name string) func(got func([]qqmusic.Track)) {
	page := 0
	return func(got func([]qqmusic.Track)) {
		var fetch func() ([]qqmusic.Track, error)
		switch name {
		case "guess":
			fetch = guessTracks
		case "radar":
			fetch = func() ([]qqmusic.Track, error) {
				page++
				songs, _, err := radarTracks(page)
				return songs, err
			}
		default:
			got(nil)
			return
		}
		go func() {
			// Some of what it gives the listener may not play: ask again
			// for those.
			var tracks []qqmusic.Track
			for tries := 0; tries < 3 && len(tracks) == 0; tries++ {
				more, err := fetch()
				if err != nil {
					break
				}
				tracks = playable(more)
			}
			a.post(func() { got(tracks) })
		}()
	}
}

// playFM plays the radio chosen, or goes on playing it.
func (a *app) playFM() {
	name := fmModes[a.fmMode].mode
	if a.pl.Radio() == name {
		if !a.pl.Playing() {
			a.pl.TogglePlay()
		}
		return
	}
	if name == "guess" && a.profile == nil {
		a.openLogin()
		return
	}
	a.radioStarting = name
	feed := a.radioFeed(name)
	feed(func(tracks []qqmusic.Track) {
		if a.radioStarting != name {
			return
		}
		a.radioStarting = ""
		if len(tracks) == 0 {
			a.toast = "个性电台暂时无法播放，请稍后再试"
			return
		}
		a.pl.PlayRadio(name, tracks, feed)
	})
}

// exitRadio stops the radio playing, but not the song it plays.
func (a *app) exitRadio() {
	if a.pl.Radio() == "" {
		return
	}
	a.pl.EndRadio()
	a.toast = "已退出个性电台"
}

// trashFM skips the song playing on the radio, telling QQ Music the
// listener does not like it.
func (a *app) trashFM() {
	tr := a.pl.Current()
	if tr == nil {
		return
	}
	song := *tr
	a.pl.Next()
	go qqmusic.Dislike(song)
}

// fmPage is the personal radio: what it plays, large, over its cover's
// colors, with the radio's own controls.
func (a *app) fmPage(c *ui.Context) {
	top, bottom := a.insets(c)
	ui.Scroll(c.Key("fm")).TrackScroll(&a.pageScroll).Grow(1).Padding(top+pageGap, pagePad, bottom+48, pagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		a.startMark(c)
		defer a.endMark(c)
		// The radio playing, if one plays.
		for i, m := range fmModes {
			if a.pl.Radio() == m.mode {
				a.fmMode = i
			}
		}
		ui.Row(c).AlignItems(ui.End).Children(func() {
			ui.Box(c).Grow(1).Children(func() { a.heading(c, "个性电台", "无尽推荐") })
			mode := a.fmMode
			ui.Box(c).Margin(0, 0, 22, 0).Children(func() {
				labels := make([]string, len(fmModes))
				for i, m := range fmModes {
					labels[i] = m.label
				}
				if segmented(c.Key("fm-mode"), &mode, labels...) {
					a.fmMode = mode
					// Playing a radio, it changes to the one chosen at once.
					if a.pl.Radio() != "" {
						a.playFM()
					}
				}
			})
		})
		a.fmCard(c)
	})
}

// fmCard shows the song the radio plays, or how to start it.
func (a *app) fmCard(c *ui.Context) {
	pl := a.pl
	onFM := pl.Radio() != ""
	var tr *qqmusic.Track
	if onFM {
		tr = pl.Current()
	}
	var art *artwork
	if tr != nil {
		art = a.art.get(qqmusic.Sized(tr.Album.PicURL, 600), true)
	}
	card := ui.Box(c.Key("fm-card")).Height(320).Radius(24).Background(ui.Hex("#232328")).TextColor(white).
		Shadow(0, 18, 44, -16, a.pal.cardShade)
	in := artIn(card, backdrop(art))
	card.Children(func() {
		if in < 1 {
			ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(24).
				LinearGradient(ui.LinearGradient{Angle: 135, Oklab: true, From: ui.Oklch(0.42, 0.14, 20), To: ui.Oklch(0.3, 0.1, 300)})
		}
		artImage(c, backdrop(art), 24, in)
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(24).
			LinearGradient(ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
				{Color: ui.RGBA(0, 0, 0, 0.35), At: 0},
				{Color: ui.RGBA(0, 0, 0, 0.1), At: 1},
			}})
		ui.Row(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Padding(36, 40).Gap(40).AlignItems(ui.Center).Children(func() {
			a.fmCovers(c, tr)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
				label := radioLabel(fmModes[a.fmMode].mode)
				if onFM {
					label = radioLabel(pl.Radio())
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconRadio).Size(14, 14).TextColor(white.Alpha(0.75))
					ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(white.Alpha(0.75))
				})
				if tr == nil {
					blurb := "根据你的口味，一首接一首，不停推荐"
					if fmModes[a.fmMode].mode == "radar" {
						blurb = "从你收藏的歌曲出发，发现风格相似的好歌"
					}
					ui.Text(c, "听见更多好音乐").FontSize(30).FontWeight(800).LetterSpacing(-0.6)
					ui.Text(c, blurb).FontSize(13.5).TextColor(white.Alpha(0.75))
					ui.Row(c).Margin(18, 0, 0, 0).Children(func() {
						label := "开始收听"
						switch {
						case a.radioStarting != "":
							label = "正在调频…"
						case fmModes[a.fmMode].mode == "guess" && a.profile == nil:
							label = "登录后收听"
						}
						if pillButton(c.Key("start"), iconPlay, label, white, ui.Hex("#1d1d1f")).Clicked() {
							a.playFM()
						}
					})
					return
				}
				ui.Text(c, tr.Name).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
				switch {
				case pl.Loading():
					ui.Text(c, "正在缓冲…").FontSize(14).TextColor(white.Alpha(0.7)).SingleLine()
				case pl.Err() != "":
					ui.Text(c, pl.Err()).FontSize(14).TextColor(white).SingleLine()
				default:
					a.artistLinks(c, tr, white.Alpha(0.7), white, true).FontSize(14).SingleLine()
				}
				ui.Row(c).Gap(14).AlignItems(ui.Center).Margin(20, 0, 0, -8).Children(func() {
					if a.profile != nil {
						if iconButton(c.Key("trash"), iconTrash, 20, white.Alpha(0.8), "不喜欢").Clicked() {
							a.trashFM()
						}
					}
					a.likeButton(c.Key("like"), tr, 20, white.Alpha(0.8))
					play := ui.ButtonBase(c.Key("play")).Size(56, 56).Center().Radius(999).Background(white).TextColor(ui.Hex("#1d1d1f")).Label("播放/暂停")
					pose := ui.Pose{}
					if play.Pressed() {
						pose.Scale = 0.92
					} else if play.Hovered() {
						pose.Scale = 1.05
					}
					play.AnimatePose(pose, ui.Snappy)
					play.Children(func() { playPause(c, play, pl.Playing(), 22) })
					if play.Clicked() {
						pl.TogglePlay()
					}
					if iconButton(c.Key("next"), iconNext, 20, white.Alpha(0.8), "下一首").Clicked() {
						pl.Next()
					}
				})
			})
		})
	})
}

// fmCovers stacks the cover playing over the next one's, which peeks out
// behind it, tilted, as the FM's own player shows them.
func (a *app) fmCovers(c *ui.Context, tr *qqmusic.Track) {
	const size = 232
	ui.Box(c).Size(size+36, size).Shrink(0).Children(func() {
		var next *qqmusic.Track
		if tr != nil {
			if order, at := a.pl.PlayOrder(); at >= 0 && at+1 < len(order) {
				next = &a.pl.Queue()[order[at+1]]
			}
		}
		if next != nil {
			back := ui.Box(c.Key(next.ID)).Absolute().Top(16).Left(36).Size(size-32, size-32).Radius(14).Opacity(0.55).Background(white.Alpha(0.12)).
				Shadow(0, 10, 24, -8, ui.RGBA(0, 0, 0, 0.4))
			back.AnimatePose(ui.Pose{Rotate: 6, X: 14}, ui.Animation{Spring: ui.SpringOptions{Duration: 500 * time.Millisecond, Bounce: 0.2}})
			back.Children(func() { a.coverFill(c, next.Album.PicURL, 400, 14) })
		}
		front := ui.Box(c.Key("front")).Absolute().Top(0).Left(0).Size(size, size).Radius(16).
			Background(white.Alpha(0.12)).Shadow(0, 20, 44, -10, ui.RGBA(0, 0, 0, 0.55))
		if tr == nil {
			front.Center().Children(func() {
				ui.Icon(c, iconRadio).Size(64, 64).TextColor(white.Alpha(0.85))
			})
			return
		}
		scale := float32(1)
		if !a.pl.Playing() {
			scale = 0.94
		}
		front.AnimatePose(ui.Pose{Scale: scale}, ui.Animation{Spring: ui.SpringOptions{Duration: 600 * time.Millisecond, Bounce: 0.25}})
		front.Children(func() {
			ui.Box(c.Key(tr.ID)).Fill().Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.92}}).
				Children(func() { a.coverFill(c, tr.Album.PicURL, 600, 16) })
		})
	})
}
