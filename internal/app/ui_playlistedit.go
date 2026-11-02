package app

import (
	"errors"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"qqgo/internal/qqmusic"
)

// The listener's own playlists: making them, deleting them, and adding
// songs to them or taking songs out. QQ Music edits a playlist by its
// directory, which the listener's own have.

// These edit playlists with QQ Music; tests replace them.
var (
	createPlaylist     = qqmusic.CreatePlaylist
	addToPlaylist      = qqmusic.AddToPlaylist
	removeFromPlaylist = qqmusic.RemoveFromPlaylist
	deletePlaylist     = qqmusic.DeletePlaylist
)

// nameDialog asks for the name of a playlist to make.
type nameDialog struct {
	open bool
	name string
	add  *qqmusic.Track // the song to add to the playlist made, if any
	busy bool           // QQ Music is making the playlist
}

// deleteDialog asks whether to delete one of the listener's playlists.
type deleteDialog struct {
	open bool
	id   int64
	name string
}

// editable returns the listener's playlist id if they may edit its songs,
// or nil: the liked songs' are their hearts'.
func (a *app) editable(id int64) *qqmusic.Playlist {
	for i := range a.mine {
		if a.mine[i].ID == id && a.mine[i].SpecialType != 5 && a.mine[i].DirID != 0 {
			return &a.mine[i]
		}
	}
	return nil
}

// loadMine loads the playlists the listener uid made and saved.
func (a *app) loadMine(uid int64) {
	go func() {
		made, saved, err := qqmusic.UserPlaylists()
		a.post(func() {
			if err == nil && a.profile != nil && a.profile.UserID == uid {
				a.mine, a.saved = made, saved
			}
		})
	}()
}

// playlistChanged forgets what was loaded of playlist id, which QQ Music
// changed: loaded anew if shown, and the sidebar's covers with it.
func (a *app) playlistChanged(id int64) {
	delete(a.playlists, id)
	if a.route.page == pagePlaylist && a.route.id == id {
		a.loadPlaylist(id, false)
	}
	if a.profile != nil {
		a.loadMine(a.profile.UserID)
	}
}

// newPlaylist asks for the name of a playlist to make, then adds tr to it
// if set. Signed out, it asks the listener to sign in.
func (a *app) newPlaylist(tr *qqmusic.Track) {
	if a.profile == nil {
		a.showMain()
		a.openLogin()
		return
	}
	a.naming = nameDialog{open: true, add: tr}
}

// submitName makes the playlist named.
func (a *app) submitName() {
	d := &a.naming
	name := strings.TrimSpace(d.name)
	if name == "" || d.busy {
		return
	}
	d.busy = true
	add := d.add
	go func() {
		pl, err := createPlaylist(name)
		a.post(func() {
			a.naming.busy = false
			if err != nil {
				a.toast = "创建歌单失败，请稍后再试"
				return
			}
			a.naming.open = false
			// Under the liked songs, as QQ Music lists the newest.
			at := 0
			if len(a.mine) > 0 && a.mine[0].SpecialType == 5 {
				at = 1
			}
			a.mine = slices.Insert(a.mine, at, *pl)
			pl.Tracks = nil
			a.playlists[pl.ID] = pl
			a.open(route{page: pagePlaylist, id: pl.ID})
			if add != nil {
				a.addTo(pl, add)
			}
		})
	}()
}

// confirmDelete asks whether to delete the listener's playlist pl.
func (a *app) confirmDelete(pl *qqmusic.Playlist) {
	a.deleting = deleteDialog{open: true, id: pl.ID, name: pl.Name}
}

// deleteChosen deletes the playlist the dialog asked about: at once from
// the sidebar, leaving its page, and then with QQ Music, which may refuse:
// it comes back then.
func (a *app) deleteChosen() {
	id := a.deleting.id
	a.deleting.open = false
	i := slices.IndexFunc(a.mine, func(pl qqmusic.Playlist) bool { return pl.ID == id })
	if i < 0 || a.mine[i].SpecialType == 5 {
		return
	}
	pl := a.mine[i]
	if pl.DirID == 0 {
		return
	}
	a.mine = slices.Delete(slices.Clone(a.mine), i, i+1)
	// Leave no way back to its page, and leave it for the page before.
	gone := func(r route) bool { return r.page == pagePlaylist && r.id == id }
	a.history = slices.DeleteFunc(a.history, gone)
	a.forward = slices.DeleteFunc(a.forward, gone)
	if gone(a.route) {
		r := route{page: pageHome}
		if n := len(a.history); n > 0 {
			r, a.history = a.history[n-1], a.history[:n-1]
		}
		a.show(r)
	}
	go func() {
		err := deletePlaylist(pl.DirID)
		a.post(func() {
			if err != nil {
				a.mine = slices.Insert(a.mine, min(i, len(a.mine)), pl)
				a.toast = "删除失败，请稍后再试"
				return
			}
			delete(a.playlists, id)
			a.toast = "已删除歌单"
		})
	}()
}

// addTo adds tr to the listener's playlist pl; a toast says how it went.
func (a *app) addTo(pl *qqmusic.Playlist, tr *qqmusic.Track) {
	pid, dir, song := pl.ID, pl.DirID, *tr
	go func() {
		err := addToPlaylist(dir, song)
		a.post(func() {
			switch {
			case errors.Is(err, qqmusic.ErrInPlaylist):
				a.toast = "歌曲已在歌单中"
			case err != nil:
				a.toast = "添加失败，请稍后再试"
			default:
				a.toast = "已添加到歌单"
				a.playlistChanged(pid)
			}
		})
	}()
}

// removeFrom takes tr out of the listener's playlist pid, at once in the
// app and then with QQ Music, which may refuse: the song comes back then.
func (a *app) removeFrom(pid int64, tr *qqmusic.Track) {
	mine := a.editable(pid)
	if mine == nil {
		return
	}
	id, dir, song := tr.ID, mine.DirID, *tr
	pl := a.playlists[pid]
	var before []qqmusic.Track
	if pl != nil {
		// A new list: the player may be playing the old one.
		before = pl.Tracks
		pl.Tracks = slices.DeleteFunc(slices.Clone(before), func(t qqmusic.Track) bool { return t.ID == id })
	}
	go func() {
		err := removeFromPlaylist(dir, song)
		a.post(func() {
			if err != nil {
				if pl != nil && a.playlists[pid] == pl {
					pl.Tracks = before
				}
				a.toast = "移除失败，请稍后再试"
				return
			}
			// The songs listed are right: only the sidebar's covers may
			// have changed.
			if a.profile != nil {
				a.loadMine(a.profile.UserID)
			}
		})
	}()
}

// playlistItems are the items of a song's menu that put it in the
// listener's playlists, or take it out of pid, the one it is listed in, if
// set.
func (a *app) playlistItems(m *ui.Menu, tr *qqmusic.Track, pid int64) {
	if a.profile == nil {
		return
	}
	m.Separator()
	m.Submenu("添加到歌单", func(m *ui.Menu) {
		if m.Item("新建歌单…").Chosen() {
			a.newPlaylist(tr)
		}
		var to []*qqmusic.Playlist
		for i := range a.mine {
			if pl := &a.mine[i]; pl.SpecialType != 5 && pl.DirID != 0 && pl.ID != pid {
				to = append(to, pl)
			}
		}
		if len(to) > 0 {
			m.Separator()
		}
		for _, pl := range to {
			if m.Item(pl.Name).Chosen() {
				a.addTo(pl, tr)
			}
		}
	})
	if a.editable(pid) != nil && m.Item("从歌单中移除").Chosen() {
		a.removeFrom(pid, tr)
	}
}

// playlistNameDialog asks for the name of a playlist to make.
func (a *app) playlistNameDialog(c *ui.Context) {
	p := a.pal
	d := &a.naming
	ui.DialogBase(c.Key("playlist-name"), &d.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(360).Padding(24).Radius(18).Gap(16).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		title, action := "新建歌单", "创建"
		if d.busy {
			action = "正在创建…"
		}
		ui.Text(c, title).FontSize(17).FontWeight(700)
		in := ui.TextInput(c.Key("name"), &d.name).Placeholder("歌单名称").Label("歌单名称").FontSize(14).AutoFocus()
		if in.Submitted() {
			a.submitName()
		}
		ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
			if pillButton(c.Key("cancel"), nil, "取消", p.hover.Over(p.bg), p.text).Clicked() {
				d.open = false
			}
			ok := pillButton(c.Key("ok"), nil, action, p.accent, p.onAccent)
			if strings.TrimSpace(d.name) == "" {
				ok.Disabled(true).Opacity(0.5)
			}
			if ok.Clicked() {
				a.submitName()
			}
		})
	})
}

// deletePlaylistDialog asks whether to delete a playlist, which can't be
// undone.
func (a *app) deletePlaylistDialog(c *ui.Context) {
	p := a.pal
	d := &a.deleting
	ui.DialogBase(c.Key("playlist-delete"), &d.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(360).Padding(24).Radius(18).Gap(8).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		ui.Text(c, "删除歌单").FontSize(17).FontWeight(700)
		ui.Text(c, "确定删除歌单“"+d.name+"”吗？删除后无法恢复。").FontSize(13).TextColor(p.muted).LineHeight(1.45)
		ui.Row(c).Gap(10).Justify(ui.End).Margin(8, 0, 0, 0).Children(func() {
			if pillButton(c.Key("cancel"), nil, "取消", p.hover.Over(p.bg), p.text).Clicked() {
				d.open = false
			}
			if pillButton(c.Key("ok"), nil, "删除", p.accent, p.onAccent).Clicked() {
				a.deleteChosen()
			}
		})
	})
}
