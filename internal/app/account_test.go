package app

import (
	"errors"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"qqgo/internal/qqmusic"
)

// Clicking the account row drops its menu, which takes the focus so the
// arrows walk its items, and Escape or a second click puts it away.
func TestAccountMenu(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &qqmusic.Profile{UserID: 1, Nickname: "小明"}
	a.mine = []qqmusic.Playlist{{ID: 5, DirID: 201, Name: "我喜欢的音乐", SpecialType: 5}}
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()

	if err := tt.Click("小明"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.accountOpen || !tt.HasText("退出登录") {
		t.Fatalf("menu not open: %v", a.accountOpen)
	}
	if !tt.Focused("我喜欢的音乐") {
		t.Fatal("the first item has no focus")
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if !tt.Focused("退出登录") {
		t.Fatal("Down did not move to signing out")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.accountOpen {
		t.Fatal("Escape left the menu open")
	}

	tt.Click("小明")
	tt.Frame()
	tt.Click("小明")
	tt.Frame()
	if a.accountOpen {
		t.Fatal("a second click left the menu open")
	}
}

// 我的喜欢的音乐 in the account menu opens the liked songs and puts the
// menu away.
func TestAccountMenuOpensLikedSongs(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.homeLoading = true
	a.profile = &qqmusic.Profile{UserID: 1, Nickname: "小明"}
	a.mine = []qqmusic.Playlist{{ID: 5, DirID: 201, Name: "我喜欢的音乐", SpecialType: 5}}
	a.loadingPl[5] = true
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()
	tt.Click("小明")
	tt.Frame()
	if err := tt.Click("我喜欢的音乐"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route != (route{page: pagePlaylist, id: 5}) || a.accountOpen {
		t.Errorf("on %+v, menu open %v", a.route, a.accountOpen)
	}
}

// The login dialog's tabs start a login with the code of the app chosen,
// and the code shown is the one it gave.
func TestLoginKinds(t *testing.T) {
	kinds := make(chan qqmusic.LoginKind, 4)
	startQR = func(kind qqmusic.LoginKind) (*qqmusic.QRLogin, error) {
		kinds <- kind
		return nil, errors.New("offline")
	}
	defer func() { startQR = qqmusic.StartQR }()
	a := newTestApp()
	a.settings = defaultSettings()
	a.homeLoading = true
	a.openLogin()
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()
	if got := wait(t, kinds, "the first code"); got != qqmusic.LoginQQMusic {
		t.Fatalf("started with %v", got)
	}
	if err := tt.Click("微信"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := wait(t, kinds, "the WeChat code"); got != qqmusic.LoginWeChat {
		t.Errorf("the tab started %v", got)
	}
	if !a.login.open || a.login.kind != 2 {
		t.Errorf("login %+v", a.login)
	}
}

// With NM_FRAMES set, saves the foot of the sidebar with the account menu
// open, light and dark.
func TestAccountMenuFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		a := newTestApp()
		a.settings = defaultSettings()
		a.profile = &qqmusic.Profile{UserID: 1, Nickname: "小明"}
		tt := ui.NewTester(a.view, 1000, 600)
		tt.SetDark(dark)
		tt.Frame()
		tt.Click("小明")
		tt.Advance(time.Second)
		tt.Frame()
		img := tt.Image()
		s := float32(img.Bounds().Dx()) / 1000
		crop := image.Rect(0, int(400*s), int((sidebarWidth+20)*s), img.Bounds().Dy())
		out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
		draw.Draw(out, out.Bounds(), img, crop.Min, draw.Src)
		name := "account-light.png"
		if dark {
			name = "account-dark.png"
		}
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, out)
		f.Close()
	}
}
