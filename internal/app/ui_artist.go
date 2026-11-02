package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"qqgo/internal/qqmusic"
)

// artistLinks shows a track's artists as its credits read, each a link to
// their page in color, brightening to hover under the pointer, and then,
// with album, a link to its album. An artist QQ Music has no page for,
// credited by name alone, is plain text.
func (a *app) artistLinks(c *ui.Context, tr *qqmusic.Track, color, hover ui.Color, album ...bool) ui.Element {
	return ui.RichText(c).TextColor(color).Children(func() {
		for i, ar := range tr.Artists {
			if i > 0 {
				ui.Text(c, " / ")
			}
			if ar.MID != "" {
				a.link(c.Key(i), ar.Name, color, hover, route{page: pageArtist, mid: ar.MID})
			} else {
				ui.Text(c, ar.Name)
			}
		}
		if len(album) > 0 && album[0] && tr.Album.Name != "" {
			ui.Text(c, " — ")
			if tr.Album.MID != "" {
				a.link(c.Key("album"), tr.Album.Name, color, hover, route{page: pageAlbum, mid: tr.Album.MID})
			} else {
				ui.Text(c, tr.Album.Name)
			}
		}
	})
}

// link is text in a RichText that opens a page.
func (a *app) link(c *ui.Context, text string, color, hover ui.Color, r route) {
	link := ui.Link(c, text, "").TextColor(color)
	if link.Hovered() {
		link.TextColor(hover)
	}
	// Once built: a song's row is in a list, which a new page resets.
	link.OnClick(func() { a.openPage(r) })
}

// openPage opens a page in the main window, from wherever a link to it
// shows: over Now Playing, which closes, or the notch.
func (a *app) openPage(r route) {
	a.open(r)
	a.showMain()
	a.redraw()
}

// openArtist opens an artist's page in the main window.
func (a *app) openArtist(mid string) { a.openPage(route{page: pageArtist, mid: mid}) }

// These load an artist, their songs and albums from QQ Music, and follow
// them; tests replace them.
var (
	artistDetail      = qqmusic.ArtistDetail
	artistLatestSongs = qqmusic.ArtistLatestSongs
	albumsBy          = qqmusic.ArtistAlbums
	followArtist      = qqmusic.FollowArtist
)

// artistData is what the app has of an artist: who they are, with their
// most popular songs, and their newest songs and albums once asked for.
type artistData struct {
	profile       *qqmusic.ArtistProfile
	loading       bool
	err           string
	latest        []qqmusic.Track
	latestLoading bool
	latestErr     string
	albums        []qqmusic.AlbumProfile
	albumsPage    paged
	following     bool // a follow or unfollow is on its way
}

// What an artist's page lists: their songs, the most popular or the
// newest, or their albums.
const (
	artistHot = iota
	artistLatest
	artistAlbums
)

// latestCount is how many of an artist's newest songs their page lists;
// albumsPage how many albums load at a time.
const (
	latestCount = 100
	albumsPage  = 30
)

func (a *app) artist(mid string) *artistData {
	d := a.artists[mid]
	if d == nil {
		d = &artistData{}
		a.artists[mid] = d
	}
	return d
}

// loadArtist loads an artist's page, and what it lists.
func (a *app) loadArtist(mid string) {
	d := a.artist(mid)
	switch a.artistTab {
	case artistLatest:
		a.loadLatest(mid)
	case artistAlbums:
		if !d.albumsPage.loaded {
			a.loadAlbums(mid)
		}
	}
	if d.profile != nil || d.loading {
		return
	}
	d.loading, d.err = true, ""
	go func() {
		ar, err := artistDetail(mid)
		a.post(func() {
			d.loading = false
			if err != nil {
				d.err = "歌手加载失败"
				return
			}
			d.profile = ar
		})
	}()
}

func (a *app) loadLatest(mid string) {
	d := a.artist(mid)
	if d.latest != nil || d.latestLoading {
		return
	}
	d.latestLoading, d.latestErr = true, ""
	go func() {
		songs, err := artistLatestSongs(mid, latestCount)
		a.post(func() {
			d.latestLoading = false
			if err != nil {
				d.latestErr = "歌曲加载失败"
				return
			}
			d.latest = songs
		})
	}()
}

// loadAlbums loads the next page of an artist's albums.
func (a *app) loadAlbums(mid string) {
	d := a.artist(mid)
	if d.albumsPage.loading {
		return
	}
	d.albumsPage.loading, d.albumsPage.err = true, ""
	offset := len(d.albums)
	go func() {
		albums, more, err := albumsBy(mid, albumsPage, offset)
		a.post(func() {
			d.albumsPage.loading = false
			if err != nil {
				d.albumsPage.err = "专辑加载失败"
				return
			}
			d.albums = append(d.albums, albums...)
			d.albumsPage.loaded, d.albumsPage.more = true, more && len(albums) > 0
		})
	}()
}

// toggleFollow follows an artist, or unfollows them, at once, going back if
// QQ Music refuses. Signed out, it asks the listener to sign in.
func (a *app) toggleFollow(mid string) {
	d := a.artist(mid)
	if d.profile == nil || d.following {
		return
	}
	if a.profile == nil {
		a.openLogin()
		return
	}
	ar := d.profile
	follow := !ar.Followed
	ar.Followed, d.following = follow, true
	go func() {
		err := followArtist(mid, follow)
		a.post(func() {
			d.following = false
			switch {
			case err != nil:
				ar.Followed = !follow
				a.toast = "操作失败，请稍后再试"
			case follow:
				a.toast = "已关注"
			default:
				a.toast = "已取消关注"
			}
		})
	}()
}

// artistPage shows who an artist is over their songs, the most popular or
// the newest, or their albums.
func (a *app) artistPage(c *ui.Context) {
	mid := a.route.mid
	d := a.artist(mid)
	if d.profile == nil {
		if !d.loading {
			a.centered(c, d.err, func() { a.loadArtist(mid) })
			return
		}
		s := a.pageShimmer(c)
		a.trackSkeleton(c, s, true, func(c *ui.Context) { a.artistHeaderSkeleton(c, s) })
		return
	}
	header := func(c *ui.Context) { a.artistHeader(c, d) }
	switch a.artistTab {
	case artistAlbums:
		a.scrollPage(c, func() {
			header(c)
			a.albumGrid(c, d.albums)
			a.pageFoot(c, &d.albumsPage, func() { a.loadAlbums(mid) })
			if d.albumsPage.loaded && len(d.albums) == 0 {
				a.empty(c, "暂无专辑")
			}
		})
		return
	}
	songs := d.profile.Songs
	if a.artistTab == artistLatest {
		switch {
		case d.latest != nil:
			songs = d.latest
		case d.latestLoading:
			a.trackSkeleton(c, a.pageShimmer(c), true, header)
			return
		default:
			songs = nil
			header = func(c *ui.Context) {
				a.artistHeader(c, d)
				ui.Column(c).Padding(40, 0).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, d.latestErr).TextColor(a.pal.muted).FontSize(13)
					if ui.Button(c.Key("retry"), "重试").Clicked() {
						a.loadLatest(d.profile.MID)
					}
				})
			}
		}
	}
	a.trackRows(c, songs, true, header)
}

// scrollPage scrolls a page of cards and rows, inset as one of tracks is:
// a tab of a page that lists tracks puts its header in the same place.
func (a *app) scrollPage(c *ui.Context, content func()) {
	top, bottom := a.insets(c)
	ui.Scroll(c.Key(a.route)).TrackScroll(&a.pageScroll).Grow(1).Padding(top+pageGap, pagePad-12, bottom+32, pagePad-12).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		a.startMark(c)
		defer a.endMark(c)
		content()
	})
}

// empty says a list has nothing in it.
func (a *app) empty(c *ui.Context, msg string) {
	ui.Text(c, msg).FontSize(13).TextColor(a.pal.muted).Padding(40, 0).AlignSelf(ui.Center)
}

// artistPhotoSize is the width of an artist's round photo on their page.
const artistPhotoSize = 212

func (a *app) artistHeader(c *ui.Context, d *artistData) {
	p := a.pal
	ar := d.profile
	// What the buttons play: the songs listed, or else the most popular.
	songs := ar.Songs
	if a.artistTab == artistLatest && d.latest != nil {
		songs = d.latest
	}
	ui.Row(c).Gap(28).AlignItems(ui.End).Padding(0, 0, 8, 0).Children(func() {
		photo := ar.Avatar
		if photo == "" {
			photo = ar.PicURL
		}
		ui.Box(c).Size(artistPhotoSize, artistPhotoSize).Radius(artistPhotoSize/2).Shadow(0, 18, 40, -10, p.cardShade).Children(func() {
			a.coverFill(c, photo, 600, artistPhotoSize/2)
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, "歌手").FontSize(12).FontWeight(600).TextColor(p.accent)
			ui.Text(c, ar.Name).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
			var meta []string
			if aka := distinct(append([]string{ar.Trans}, ar.Alias...)); len(aka) > 0 {
				meta = append(meta, strings.Join(aka, " / "))
			}
			if ar.MusicSize > 0 {
				meta = append(meta, fmt.Sprintf("%d 首歌曲", ar.MusicSize))
			}
			if ar.AlbumSize > 0 {
				meta = append(meta, fmt.Sprintf("%d 张专辑", ar.AlbumSize))
			}
			ui.Text(c, strings.Join(meta, " · ")).FontSize(13).TextColor(p.muted)
			if d := strings.TrimSpace(ar.BriefDesc); d != "" {
				ui.Text(c, strings.ReplaceAll(d, "\n", " ")).FontSize(12.5).TextColor(p.muted).MaxLines(2).LineHeight(1.45).Margin(2, 0, 0, 0)
			}
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				if pillButton(c.Key("play"), iconPlay, "播放", p.accent, p.onAccent).Clicked() {
					if a.pl.Shuffle() {
						a.pl.ToggleShuffle()
					}
					a.pl.PlayQueue(songs, firstPlayable(songs))
				}
				if pillButton(c.Key("shuffle"), iconShuffle, "随机播放", p.hover.Over(p.bg), p.text).Clicked() {
					if !a.pl.Shuffle() {
						a.pl.ToggleShuffle()
					}
					a.pl.PlayQueue(songs, firstPlayable(songs))
				}
				icon, label := iconPlus, "关注"
				if ar.Followed {
					icon, label = iconCheck, "已关注"
				}
				if pillButton(c.Key("follow"), icon, label, p.hover.Over(p.bg), p.text).Clicked() {
					a.toggleFollow(ar.MID)
				}
			})
		})
	})
	ui.Row(c).AlignItems(ui.Center).Margin(34, 0, 14, 0).Children(func() {
		ui.Text(c, artistTabTitles[a.artistTab]).FontSize(19).FontWeight(700).LetterSpacing(-0.3)
		ui.Spacer(c)
		if segmented(c.Key("tab"), &a.artistTab, "热门", "最新", "专辑") {
			a.loadArtist(ar.MID)
		}
	})
}

var artistTabTitles = [...]string{"歌曲", "歌曲", "专辑"}

func (a *app) artistHeaderSkeleton(c *ui.Context, s shimmer) {
	ui.Row(c).Gap(28).AlignItems(ui.End).Padding(0, 0, 8, 0).Children(func() {
		s.bone(c, artistPhotoSize/2).Size(artistPhotoSize, artistPhotoSize)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			s.text(c, 12, 1.3).Width(24)
			s.text(c, 30, 1.2).WidthPercent(32)
			s.text(c, 13, 1.3).Width(160)
			ui.Column(c).Margin(2, 0, 0, 0).Children(func() {
				s.text(c, 12.5, 1.45).WidthPercent(78)
				s.text(c, 12.5, 1.45).WidthPercent(52)
			})
			ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
				s.bone(c, 18).Size(92, 36)
				s.bone(c, 18).Size(120, 36)
			})
		})
	})
	a.section(c, "歌曲")
}

// distinct returns ss without empty strings, nor any after its first: an
// artist's translated name is often among their aliases too.
func distinct(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
