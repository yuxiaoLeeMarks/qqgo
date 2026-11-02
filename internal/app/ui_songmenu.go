package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"qqgo/internal/qqmusic"
)

// copyText puts text on the clipboard; tests replace it.
var copyText = func(text string) error { return mygo.Clipboard.Write(transfer.TextData(text)) }

// songMenu is three dots that open the menu of tr.
func (a *app) songMenu(c *ui.Context, tr *qqmusic.Track, size float32, color ui.Color) ui.Element {
	b := iconButton(c, iconMore, size, color, "更多")
	b.Menu(a.songItems(tr, 0))
	return b
}

// songItems builds the menu of what can be done with tr, behind its dots
// or a right click on its row: copying its title, its title with its
// artists, or the link to its page, opening its artists' or album's, and,
// signed in, adding it to a playlist or taking it out of pid, the playlist
// it is listed in, if set.
func (a *app) songItems(tr *qqmusic.Track, pid int64) func(m *ui.Menu) {
	return func(m *ui.Menu) {
		if m.Item("复制歌名").Chosen() {
			copyText(tr.Name)
		}
		if m.Item("复制歌名和歌手").Chosen() {
			copyText(tr.Name + " - " + tr.ArtistNames())
		}
		m.Separator()
		if m.Item("复制链接").Chosen() {
			copyText(tr.URL())
		}
		// Its artists' and album's pages, those QQ Music has.
		linked := tr.Album.MID != ""
		for _, ar := range tr.Artists {
			linked = linked || ar.MID != ""
		}
		if linked {
			m.Separator()
		}
		for _, ar := range tr.Artists {
			if ar.MID != "" && m.Item("查看歌手："+ar.Name).Chosen() {
				a.openArtist(ar.MID)
			}
		}
		if tr.Album.MID != "" && m.Item("查看专辑："+tr.Album.Name).Chosen() {
			a.openPage(route{page: pageAlbum, mid: tr.Album.MID})
		}
		a.playlistItems(m, tr, pid)
	}
}
