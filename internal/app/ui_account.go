package app

import (
	"bytes"
	"image"
	"image/draw"
	"runtime"
	"time"

	"github.com/egoist/mygo/ui"

	"qqgo/internal/qqmusic"
)

// loginState is a QR code login under way.
type loginState struct {
	open     bool
	gen      int // counts logins, to stop the polls of closed ones
	kind     int // the app it scans with, in loginKinds
	qr       *qqmusic.QRLogin
	code     *ui.Bitmap // the code, scaled up crisply
	status   qqmusic.QRState
	err      string
	finished bool
}

// loginKinds are the apps a code is scanned with, as the dialog's tabs
// name them, and what it asks the listener to do.
var loginKinds = []struct {
	kind       qqmusic.LoginKind
	label, how string
}{
	{qqmusic.LoginQQMusic, "QQ 音乐", "打开 QQ 音乐 App，扫一扫登录"},
	{qqmusic.LoginQQ, "QQ", "打开手机 QQ，扫一扫登录"},
	{qqmusic.LoginWeChat, "微信", "打开微信，扫一扫登录"},
}

// startQR starts a login; tests replace it.
var startQR = qqmusic.StartQR

// boot signs in with the login in the keychain, if any, then loads the
// home page for whoever is listening.
func (a *app) boot() {
	a.homeLoading = true
	// A login QQ Music stops taking, as it expires unrenewed, signs out.
	qqmusic.Revoked = func() {
		a.post(func() {
			if a.profile != nil {
				a.signedIn(nil)
				a.toast = "登录已失效，请重新登录"
			}
		})
	}
	go func() {
		var prof *qqmusic.Profile
		if qqmusic.RestoreSession() {
			prof, _ = qqmusic.Account()
		}
		a.post(func() {
			a.signedIn(prof)
		})
	}()
}

// signedIn shows what is personal to prof, or to nobody.
func (a *app) signedIn(prof *qqmusic.Profile) {
	a.profile = prof
	// Playback picks up where it was left, once the login that lets the
	// listener play their songs is back.
	a.restoreSession()
	a.mine, a.saved, a.liked = nil, nil, nil
	// What the listener may play, and follows, changed: load the pages
	// again.
	a.playlists = map[int64]*qqmusic.Playlist{}
	a.plErr = map[int64]string{}
	a.artists, a.albums = map[string]*artistData{}, map[string]*albumData{}
	a.searches = map[route]*searchData{}
	a.load(a.route)
	a.loadHome()
	if prof == nil {
		return
	}
	a.loadLiked(prof.UserID)
	a.loadMine(prof.UserID)
}

// openLogin opens the login dialog on the code of the app last chosen.
func (a *app) openLogin() { a.startLogin(a.login.kind) }

// startLogin shows a code to scan with the app of loginKinds[kind].
func (a *app) startLogin(kind int) {
	if a.login.qr != nil {
		a.login.qr.Close()
	}
	a.login = loginState{open: true, gen: a.login.gen + 1, kind: kind}
	gen := a.login.gen
	go func() {
		q, err := startQR(loginKinds[kind].kind)
		var code *ui.Bitmap
		if err == nil {
			code = qrBitmap(q.Image)
		}
		a.post(func() {
			if gen != a.login.gen {
				if q != nil {
					q.Close()
				}
				return
			}
			if err != nil || code == nil {
				a.login.err = "无法获取二维码，请检查网络"
				return
			}
			a.login.qr, a.login.code, a.login.status = q, code, qqmusic.QRWaiting
			a.pollLogin(gen, q)
		})
	}()
}

// qrBitmap decodes a code's image, scaled up by whole pixels so that its
// modules stay sharp as it is drawn larger than it came.
func qrBitmap(b []byte) *ui.Bitmap {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	r := img.Bounds()
	n := max(1, 640/max(r.Dx(), 1))
	src := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(src, src.Bounds(), img, r.Min, draw.Src)
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx()*n, r.Dy()*n))
	for y := range dst.Rect.Dy() {
		for x := range dst.Rect.Dx() {
			i, j := dst.PixOffset(x, y), src.PixOffset(x/n, y/n)
			copy(dst.Pix[i:i+4], src.Pix[j:j+4])
		}
	}
	return ui.NewBitmap(dst)
}

// pollLogin follows the login until it ends or the dialog closes: each
// poll waits a little for the code to be scanned, or the login confirmed.
func (a *app) pollLogin(gen int, q *qqmusic.QRLogin) {
	go func() {
		defer q.Close()
		for {
			state, err := q.Poll()
			var prof *qqmusic.Profile
			if err == nil && state == qqmusic.QRConfirmed {
				prof, _ = qqmusic.Account()
			}
			done := make(chan bool, 1)
			a.post(func() {
				l := &a.login
				if gen != l.gen || !l.open {
					done <- true
					return
				}
				switch {
				case err != nil:
					l.err = err.Error()
				case state == qqmusic.QRConfirmed:
					l.status, l.finished = state, true
					a.signedIn(prof)
					a.closeLoginSoon(gen)
				default:
					l.status = state
				}
				done <- l.status == qqmusic.QRExpired || l.finished || l.err != ""
			})
			if <-done {
				return
			}
		}
	}()
}

// closeLoginSoon closes the dialog a moment after it showed the welcome.
func (a *app) closeLoginSoon(gen int) {
	go func() {
		time.Sleep(1200 * time.Millisecond)
		a.post(func() {
			if gen == a.login.gen {
				a.login.open = false
			}
		})
	}()
}

func (a *app) signOut() {
	a.login.gen++
	a.signedIn(nil)
	go qqmusic.Logout()
}

// loginDialog shows the QR code to scan with the QQ Music app, QQ or
// WeChat on a phone.
func (a *app) loginDialog(c *ui.Context) {
	p := a.pal
	l := &a.login
	ui.DialogBase(c.Key("login"), &l.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(340).Padding(28, 28, 26, 28).Radius(22).Gap(6).AlignItems(ui.Center).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})
		title, sub := "扫码登录", loginKinds[l.kind].how
		switch {
		case l.finished:
			title, sub = "登录成功", "欢迎回来，"+a.profileName()
		case l.err != "":
			sub = l.err
		case l.status == qqmusic.QRScanned:
			title, sub = "扫描成功", "请在手机上确认登录"
		case l.status == qqmusic.QRExpired:
			sub = "二维码已失效"
		}
		ui.Text(c, title).FontSize(20).FontWeight(700)
		ui.Text(c, sub).FontSize(13).TextColor(p.muted).TextAlign(ui.Center).Margin(0, 0, 10, 0)
		if !l.finished {
			labels := make([]string, len(loginKinds))
			for i, k := range loginKinds {
				labels[i] = k.label
			}
			kind := l.kind
			ui.Box(c).Margin(0, 0, 14, 0).Children(func() {
				if segmented(c.Key("kind"), &kind, labels...) {
					a.startLogin(kind)
				}
			})
		}

		ui.Box(c).Size(212, 212).Radius(16).Background(ui.RGB(255, 255, 255)).Border(1, p.hairline).Children(func() {
			if l.code != nil {
				code := l.code
				opacity := float32(1)
				if l.status == qqmusic.QRScanned || l.status == qqmusic.QRExpired || l.finished || l.err != "" {
					opacity = 0.12
				}
				ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Opacity(opacity).
					Draw(func(pt *ui.Painter, r ui.Rect) {
						pt.Image(code, ui.Rect{X: r.X + 12, Y: r.Y + 12, W: r.W - 24, H: r.H - 24}, ui.Contain)
					})
			} else if l.err == "" {
				// On white whatever the appearance, as the code is.
				s := shimmer{base: ui.RGBA(0, 0, 0, 0.08), sheen: ui.RGBA(0, 0, 0, 0.03)}.in(c)
				qrSkeleton(c, s, 14).Absolute().Top(0).Left(0).Right(0).Bottom(0)
			}
			ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Gap(10).Children(func() {
				switch {
				case l.finished:
					if a.profile != nil {
						if bmp := bitmap(a.art.get(qqmusic.Sized(a.profile.AvatarURL, 160), false)); bmp != nil {
							pic := ui.Box(c).Size(72, 72).Radius(36).Shadow(0, 6, 16, -4, ui.RGBA(0, 0, 0, 0.3))
							in := artIn(pic, bmp)
							pic.Children(func() { artImage(c, bmp, 36, in) })
						}
						ui.Text(c, a.profile.Nickname).FontSize(14).FontWeight(600).TextColor(ui.Hex("#1d1d1f"))
					}
				case l.status == qqmusic.QRScanned:
					ui.Box(c).Size(56, 56).Radius(28).Center().Background(p.accent).TextColor(white).Children(func() {
						ui.Icon(c, iconCheck).Size(26, 26)
					})
				case l.status == qqmusic.QRExpired || l.err != "":
					if pillButton(c.Key("refresh"), iconRepeat, "刷新二维码", p.accent, p.onAccent).Clicked() {
						a.startLogin(l.kind)
					}
				}
			})
		})
		ui.Text(c, "登录后可播放更多歌曲，每日推荐与歌单将根据你的口味生成").FontSize(11.5).TextColor(p.faint).TextAlign(ui.Center).Margin(16, 0, 0, 0)
	})
}

func (a *app) profileName() string {
	if a.profile == nil {
		return ""
	}
	return a.profile.Nickname
}

// accountRow sits at the foot of the sidebar: who is signed in, with a
// dropdown to open the settings or sign out, or a way to sign in.
func (a *app) accountRow(c *ui.Context) {
	p := a.pal
	b := ui.ButtonBase(c.Key("account")).Height(40).PaddingX(8).Gap(10).Radius(10).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || a.accountOpen && a.profile != nil {
		b.Background(p.hover)
	}
	if a.profile == nil {
		a.accountOpen = false
		b.Label("登录").Children(func() {
			ui.Box(c).Size(26, 26).Radius(13).Background(p.press).Center().Children(func() {
				ui.Icon(c, iconUser).Size(14, 14).TextColor(p.muted)
			})
			ui.Text(c, "登录 QQ 音乐").FontSize(13).FontWeight(500).TextColor(p.muted)
		})
		if b.Clicked() {
			a.openLogin()
		}
		return
	}
	prof := a.profile
	b.Label(prof.Nickname).Expanded(a.accountOpen).Children(func() {
		a.avatar(c, prof.AvatarURL, 26, p.press)
		ui.Text(c, prof.Nickname).FontSize(13).FontWeight(600).SingleLine().Shrink(1).MinWidth(0)
		if prof.VIP {
			ui.Text(c, "VIP").FontSize(9).FontWeight(800).TextColor(p.accent).Padding(0, 4).Radius(3).Border(1, p.accent.Alpha(0.6))
		}
		ui.Spacer(c)
		ui.Icon(c, iconUpDown).Size(13, 13).TextColor(p.faint)
	})
	if b.Clicked() {
		a.accountOpen = !a.accountOpen
	}
	a.accountMenu(c, b)
}

// accountMenu drops up from the account row, as wide as it, in the colors
// of the volume popover: the liked songs and the settings, then signing
// out.
func (a *app) accountMenu(c *ui.Context, row ui.Element) {
	pop := popoverColors(a.pal.dark)
	ui.PopoverBase(c.Key("account-menu"), row, &a.accountOpen, func(panel ui.Element) {
		panel.AttachTo(row, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 6, 0).Padding(5).Gap(1).
			Radius(12).Background(pop.fill).Border(0.5, pop.edge).Shadow(0, 8, 24, -4, ui.RGBA(0, 0, 0, 0.2)).
			Role(ui.RoleMenu).FocusGroup(ui.Vertical).Label("账户").TransformOrigin(0.5, 1).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.96, Y: 4}, Exit: &ui.Motion{Scale: 0.98}})
		if w := row.Bounds().W; w > 0 {
			panel.Width(w)
		}
		shortcut := "Ctrl+,"
		if runtime.GOOS == "darwin" {
			shortcut = "⌘,"
		}
		if liked := a.likedPlaylist(); liked != 0 && a.menuItem(c, iconHeart, "我喜欢的音乐", "").AutoFocus().Clicked() {
			a.accountOpen = false
			a.open(route{page: pagePlaylist, id: liked})
		}
		if a.menuItem(c, iconSettings, "设置…", shortcut).Clicked() {
			a.accountOpen = false
			a.openSettings()
		}
		ui.Box(c).Height(0.5).Margin(4, 8).Background(pop.edge)
		if a.menuItem(c, iconSignOut, "退出登录", "").Clicked() {
			a.accountOpen = false
			a.signOut()
		}
	})
}

// menuItem is an item of a dropdown: an icon, its label, and the shortcut
// that does the same, if any.
func (a *app) menuItem(c *ui.Context, icon *ui.SVG, label, shortcut string) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key(label)).Role(ui.RoleMenuItem).Label(label).Height(30).PaddingX(8).Gap(9).Radius(7).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || b.FocusVisible() {
		b.Background(p.press)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(15, 15).TextColor(p.muted)
		ui.Text(c, label).FontSize(13).Grow(1)
		if shortcut != "" {
			ui.Text(c, shortcut).FontSize(12).TextColor(p.faint)
		}
	})
	return b
}

// myPlaylists lists the listener's playlists under a header that opens
// and closes them: those they made, if mine, with a button to make one
// and a menu to delete them.
func (a *app) myPlaylists(c *ui.Context, title string, lists []qqmusic.Playlist, mine bool, collapsed *bool) {
	if len(lists) == 0 {
		return
	}
	p := a.pal
	ui.Row(c.Key(title)).AlignItems(ui.Center).Margin(18, 4, 6, 6).Children(func() {
		b := ui.ButtonBase(c.Key("toggle")).Grow(1).Height(26).PaddingX(4).Gap(6).Radius(6).
			AlignItems(ui.Center).Justify(ui.Start).Role(ui.RoleDisclosure).Label(title)
		if b.Clicked() {
			*collapsed = !*collapsed
		}
		b.Expanded(!*collapsed)
		if b.Hovered() {
			b.Background(p.hover)
		}
		b.Children(func() {
			ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(p.faint).Grow(1)
			arrow := iconDown
			if *collapsed {
				arrow = iconForward
			}
			ui.Icon(c, arrow).Size(12, 12).TextColor(p.faint)
		})
		if mine && iconButton(c.Key("new"), iconPlus, 13, p.faint, "新建歌单").Size(26, 26).Radius(6).Clicked() {
			a.newPlaylist(nil)
		}
	})
	if *collapsed {
		return
	}
	for i := range lists {
		pl := &lists[i]
		selected := a.route.page == pagePlaylist && a.route.id == pl.ID && !a.nowPlaying
		b := ui.ButtonBase(c.Key(pl.ID)).Height(34).PaddingX(8).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(pl.Name)
		switch {
		case selected:
			b.Background(p.press)
		case b.Hovered():
			b.Background(p.hover)
		}
		b.Children(func() {
			if pl.SpecialType == 5 {
				ui.Box(c).Size(22, 22).Radius(5).Center().Background(p.accent.Alpha(0.14)).Children(func() {
					ui.Icon(c, iconHeartFill).Size(12, 12).TextColor(p.accent)
				})
			} else {
				a.cover(c, pl.Cover(), 80, 22, 5)
			}
			ui.Text(c, pl.Name).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
		})
		if mine && pl.SpecialType != 5 {
			b.ContextMenu(func(m *ui.Menu) {
				if m.Item("删除歌单…").Chosen() {
					a.confirmDelete(pl)
				}
			})
		}
		if b.Clicked() {
			a.open(route{page: pagePlaylist, id: pl.ID})
		}
	}
}
