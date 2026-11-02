package qqmusic

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

// The listener's login is a Credential: their QQ Music ID and the key
// their requests carry, which expires, with what renews it. The app keeps
// it in the system's keychain. Development builds keep it in a private
// file instead: each rebuild is signed anew, and the keychain would ask
// before every one of them read it.

// Credential is a login to QQ Music, as the login server gives it.
type Credential struct {
	MusicID      int64  `json:"music_id"`
	MusicKey     string `json:"music_key"`
	OpenID       string `json:"open_id,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	RefreshKey   string `json:"refresh_key,omitempty"`
	UnionID      string `json:"union_id,omitempty"`
	// KeyCreated and KeyExpiresIn say when the key was made, in Unix
	// seconds, and for how long it is good; ExpiresAt when it ends, as
	// older logins say it.
	KeyCreated   int64 `json:"music_key_create_time,omitempty"`
	KeyExpiresIn int64 `json:"key_expires_in,omitempty"`
	ExpiresAt    int64 `json:"expires_at,omitempty"`
	// LoginType is how the listener signed in: 1 WeChat, 2 QQ.
	LoginType    int64  `json:"login_type"`
	EncryptedUIN string `json:"encrypted_uin,omitempty"`
	ClientGUID   string `json:"client_guid,omitempty"`
}

func (c *Credential) guid() string {
	if c.ClientGUID != "" {
		return c.ClientGUID
	}
	return device()
}

func (c *Credential) cookie() string {
	return fmt.Sprintf("uin=%d; qqmusic_uin=%d; qqmusic_key=%s; qm_keyst=%s; tmeLoginType=%d",
		c.MusicID, c.MusicID, c.MusicKey, c.MusicKey, c.LoginType)
}

// expiring reports whether the key runs out within five minutes of now.
func (c *Credential) expiring(now time.Time) bool {
	end := c.ExpiresAt
	if c.KeyCreated > 0 && c.KeyExpiresIn > 0 {
		end = c.KeyCreated + c.KeyExpiresIn
	}
	return end > 0 && end <= now.Unix()+300
}

const sessionSecret = "qqmusic-session"

func dataFile(name string) (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func readSession() ([]byte, error) {
	if !mygo.IsDev() {
		return mygo.Secrets.Get(sessionSecret)
	}
	path, err := dataFile("session")
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func writeSession(b []byte) error {
	if !mygo.IsDev() {
		return mygo.Secrets.Set(sessionSecret, b)
	}
	path, err := dataFile("session")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func deleteSession() {
	if !mygo.IsDev() {
		mygo.Secrets.Delete(sessionSecret)
		return
	}
	if path, err := dataFile("session"); err == nil {
		os.Remove(path)
	}
}

var (
	deviceOnce sync.Once
	deviceID   string
)

// device returns this install's GUID, made once and kept in the user data
// directory: QQ Music trusts a device it has seen before, and counts the
// devices a listener signs in on. It is no secret, so it stays out of the
// keychain.
func device() string {
	deviceOnce.Do(func() {
		// Tests make one of their own, and keep it to themselves.
		if testing.Testing() {
			deviceID = newGUID()
			return
		}
		path, _ := dataFile("guid")
		if path != "" {
			if b, err := os.ReadFile(path); err == nil && len(b) == 32 {
				deviceID = string(b)
				return
			}
		}
		deviceID = newGUID()
		if path != "" && os.MkdirAll(filepath.Dir(path), 0o700) == nil {
			os.WriteFile(path, []byte(deviceID), 0o600)
		}
	})
	return deviceID
}

// newGUID returns 32 random hexadecimal digits, as the client's GUIDs.
func newGUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

var (
	sessMu    sync.Mutex
	session   *Credential
	refreshMu sync.Mutex // one renewal of the key at a time
)

// current returns the login, or nil.
func current() *Credential {
	sessMu.Lock()
	defer sessMu.Unlock()
	return session
}

// SignedIn reports whether there is a login.
func SignedIn() bool { return current() != nil }

// fresh returns the login, renewing its key first once it nears its end.
func fresh() (*Credential, error) {
	cred := current()
	if cred == nil {
		return nil, ErrSignedOut
	}
	if !cred.expiring(time.Now()) {
		return cred, nil
	}
	refreshMu.Lock()
	defer refreshMu.Unlock()
	// Renewed, or signed out, while it waited.
	cred = current()
	if cred == nil || !cred.expiring(time.Now()) {
		if cred == nil {
			return nil, ErrSignedOut
		}
		return cred, nil
	}
	renewed, err := refresh(cred)
	if err != nil {
		var le *LoginError
		if errors.As(err, &le) && rejected(le.Code) {
			revoke(cred)
			return nil, ErrSignedOut
		}
		return nil, err
	}
	sessMu.Lock()
	if session == cred {
		session = renewed
	}
	sessMu.Unlock()
	saveSession(renewed)
	return renewed, nil
}

// revoke forgets cred, which QQ Music no longer takes, unless another
// login took its place.
func revoke(cred *Credential) {
	sessMu.Lock()
	gone := session == cred
	if gone {
		session = nil
	}
	sessMu.Unlock()
	if gone {
		deleteSession()
		if Revoked != nil {
			Revoked()
		}
	}
}

// Revoked, if set, hears that QQ Music no longer takes the login, which
// is forgotten. It is called on the goroutine of the request refused.
var Revoked func()

// RestoreSession takes up the login kept in the keychain, and reports
// whether there was one.
func RestoreSession() bool {
	b, err := readSession()
	if err != nil || len(b) == 0 {
		return false
	}
	var c Credential
	if json.Unmarshal(b, &c) != nil || c.MusicID == 0 || c.MusicKey == "" {
		return false
	}
	sessMu.Lock()
	session = &c
	sessMu.Unlock()
	return true
}

// useSession signs in with cred, keeping it in the keychain.
func useSession(cred *Credential) {
	sessMu.Lock()
	session = cred
	sessMu.Unlock()
	saveSession(cred)
}

func saveSession(cred *Credential) {
	b, _ := json.Marshal(cred)
	writeSession(b)
}

// ClearSession forgets the login, here and in the keychain.
func ClearSession() {
	sessMu.Lock()
	session = nil
	sessMu.Unlock()
	deleteSession()
}

// Logout ends the login with QQ Music too.
func Logout() {
	if cred := current(); cred != nil {
		callAs(cred, "music.login.LoginServer", "Logout", obj{}, obj{"tmeLoginType": cred.LoginType})
	}
	ClearSession()
}

// Profile is who is signed in.
type Profile struct {
	UserID    int64  `json:"userId"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
	// VIP says the listener subscribes to QQ Music's green diamond, which
	// plays its VIP songs.
	VIP bool `json:"vip"`
}

// Account returns who is signed in, or nil if nobody is, or their login
// is no longer good.
func Account() (*Profile, error) {
	cred, err := fresh()
	if errors.Is(err, ErrSignedOut) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data, err := callAs(cred, "music.UserInfo.userInfoServer", "GetLoginUserInfo", obj{}, nil)
	if errors.Is(err, ErrSignedOut) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := &Profile{UserID: cred.MusicID}
	p.Nickname = findStr(data, "nick", "nickname", "userName")
	p.AvatarURL = https(findStr(data, "headurl", "headUrl", "avatarUrl", "logo", "headpic"))
	if p.Nickname == "" || p.AvatarURL == "" {
		if legacy, err := legacyProfile(cred); err == nil {
			if p.Nickname == "" {
				p.Nickname = findStr(legacy, "nick", "nickname")
			}
			if p.AvatarURL == "" {
				p.AvatarURL = https(findStr(legacy, "headpic", "headurl", "avatarUrl"))
			}
		}
	}
	if p.Nickname == "" {
		p.Nickname = "QQ 音乐用户"
	}
	if p.AvatarURL == "" {
		p.AvatarURL = fmt.Sprintf("https://q1.qlogo.cn/g?b=qq&nk=%d&s=100", cred.MusicID)
	}
	p.VIP = vip(cred)
	return p, nil
}

// vip asks whether the listener has a green diamond.
func vip(cred *Credential) bool {
	data, err := callAs(cred, "VipLogin.VipLoginInter", "vip_login_base", obj{}, nil)
	if err != nil {
		return false
	}
	return num(get(data, "identity"), "HugeVip", "LuxVip", "SuperVip") > 0 || num(data, "vipflag", "vip_flag") > 0
}

// legacyProfile asks the older profile page, which also says the
// listener's encrypted UIN, which their liked songs are listed by.
func legacyProfile(cred *Credential) (any, error) {
	q := fmt.Sprintf("ct=19&cv=2201&format=json&cid=205360838&userid=%d&uin=%d&g_tk=%d&guid=%s",
		cred.MusicID, cred.MusicID, hash33(cred.MusicKey), cred.guid())
	req, err := newGet("https://c6.y.qq.com/rsc/fcgi-bin/fcg_get_profile_homepage.fcg?" + q)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cred.cookie())
	return send(req)
}

// withEncryptedUIN fills in cred's encrypted UIN, if it hasn't one.
func withEncryptedUIN(cred *Credential) *Credential {
	if cred.EncryptedUIN != "" {
		return cred
	}
	if p, err := legacyProfile(cred); err == nil {
		cred.EncryptedUIN = findStr(p, "encrypt_uin", "encryptUin")
	}
	return cred
}

// encryptedUIN returns the listener's encrypted UIN, asking for it once.
func encryptedUIN(cred *Credential) string {
	if cred.EncryptedUIN == "" {
		c := *cred
		withEncryptedUIN(&c)
		if c.EncryptedUIN != "" {
			sessMu.Lock()
			if session == cred {
				session = &c
			}
			sessMu.Unlock()
			saveSession(&c)
		}
		return c.EncryptedUIN
	}
	return cred.EncryptedUIN
}
