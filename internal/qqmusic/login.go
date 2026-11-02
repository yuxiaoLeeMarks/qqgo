package qqmusic

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Signing in: the listener scans a QR code with the QQ Music app, QQ or
// WeChat on their phone, which gives a ticket the login server trades for
// a Credential. The flows follow lyrune's.

// LoginError is a login the login server refused, with why.
type LoginError struct {
	Code   int64
	Detail string
}

func (e *LoginError) Error() string {
	return fmt.Sprintf("QQ 音乐登录失败（错误码 %d）：%s", e.Code, e.Detail)
}

// loginDetail says what a refusal of the login server means.
func loginDetail(code int64) string {
	switch code {
	case 1000, 104400, 104401:
		return "登录授权已过期，请重新登录"
	case 20261:
		return "登录参数无效，请重新发起登录"
	case 20271:
		return "验证码错误或已经使用"
	case 20272:
		return "账号绑定状态异常"
	case 20274:
		return "账号尚未完成必要绑定"
	case 20277, 20278:
		return "账号登录受到限制，请在 QQ 音乐官方客户端中检查账号状态"
	case 20279:
		return "登录设备数量已达上限，请在 QQ 音乐官方客户端的设备管理中移除旧设备后重试"
	case 20450:
		return "账号已被封禁，请在 QQ 音乐官方客户端中检查账号状态"
	case 104604:
		return "登录操作过于频繁，请稍后再试"
	}
	return "QQ 音乐拒绝了登录请求"
}

// loginCall asks the login server for module.method, as cred if set, with
// what of comm extra changes, and returns its whole answer.
func loginCall(cred *Credential, guid, module, method string, param, extra obj) (any, error) {
	c := obj{"ct": 19, "cv": 2201, "chid": "0", "guid": guid}
	if cred != nil {
		c["uin"] = fmt.Sprint(cred.MusicID)
		c["g_tk"] = hash33(cred.MusicKey)
	}
	for k, v := range extra {
		c[k] = v
	}
	body := marshal(obj{"comm": c, "result": obj{"module": module, "method": method, "param": param}})
	req, err := http.NewRequest("POST", gatewayURL+"?"+url.Values{"sign": {sign(body)}}.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cred != nil {
		req.Header.Set("Cookie", fmt.Sprintf("uin=%d; qqmusic_key=%s; qm_keyst=%s; tmeLoginType=%d",
			cred.MusicID, cred.MusicKey, cred.MusicKey, cred.LoginType))
	}
	return send(req)
}

// credential reads the login the login server gave in res, for the
// device guid.
func credential(res any, guid string) (*Credential, error) {
	r := get(res, "result")
	d := get(r, "data")
	if code := num(r, "code"); code != 0 {
		detail := loginDetail(code)
		for _, s := range []string{str(d, "errMsg"), str(d, "errTip2"), str(d, "errtip"), str(d, "tip3")} {
			if s = strings.TrimSpace(s); s != "" && s != detail {
				detail += "（腾讯提示：" + s + "）"
				break
			}
		}
		return nil, &LoginError{Code: code, Detail: detail}
	}
	c := &Credential{
		MusicID:      num(d, "musicid"),
		MusicKey:     str(d, "musickey"),
		OpenID:       str(d, "openid"),
		AccessToken:  str(d, "access_token"),
		RefreshToken: str(d, "refresh_token"),
		RefreshKey:   str(d, "refresh_key"),
		UnionID:      str(d, "unionid"),
		KeyCreated:   num(d, "musickeyCreateTime"),
		KeyExpiresIn: num(d, "keyExpiresIn"),
		ExpiresAt:    num(d, "expired_at"),
		LoginType:    num(d, "loginType"),
		EncryptedUIN: str(d, "encryptUin"),
		ClientGUID:   guid,
	}
	// WeChat's logins may only give the ID as a string.
	if c.MusicID == 0 {
		c.MusicID = num(d, "str_musicid")
	}
	if c.MusicID == 0 {
		return nil, errors.New("QQ 音乐登录返回了无效的账号")
	}
	if strings.TrimSpace(c.MusicKey) == "" {
		return nil, errors.New("QQ 音乐登录返回了无效的凭据")
	}
	if c.LoginType == 0 {
		c.LoginType = 2
		if strings.HasPrefix(c.MusicKey, "W_X") {
			c.LoginType = 1
		}
	}
	return c, nil
}

// refresh renews cred's key before it runs out.
func refresh(cred *Credential) (*Credential, error) {
	guid := cred.guid()
	var res any
	var err error
	if cred.LoginType == 2 && cred.OpenID != "" && cred.AccessToken != "" {
		res, err = loginCall(cred, guid, "QQConnectLogin.LoginServer", "QQLogin", obj{
			"openid": cred.OpenID, "access_token": cred.AccessToken, "refresh_token": cred.RefreshToken,
			"musicid": cred.MusicID, "musickey": cred.MusicKey, "onlyNeedAccessToken": 0, "forceRefreshToken": 0,
		}, obj{"ct": 19, "cv": 1, "tmeLoginType": 2})
	} else {
		param := obj{"refresh_key": cred.RefreshKey, "refresh_token": cred.RefreshToken, "musickey": cred.MusicKey, "musicid": cred.MusicID}
		if cred.LoginType == 1 {
			param["openid"], param["unionid"] = cred.OpenID, cred.UnionID
			param["str_musicid"], param["loginMode"] = fmt.Sprint(cred.MusicID), 2
		}
		res, err = loginCall(cred, guid, "music.login.LoginServer", "Login", param, obj{"tmeLoginType": cred.LoginType})
	}
	if err != nil {
		return nil, err
	}
	renewed, err := credential(res, guid)
	if err != nil {
		return nil, err
	}
	if renewed.EncryptedUIN == "" {
		renewed.EncryptedUIN = cred.EncryptedUIN
	}
	return renewed, nil
}

// LoginKind is what the listener scans the code with.
type LoginKind int

const (
	LoginQQMusic LoginKind = iota // the QQ Music app
	LoginQQ
	LoginWeChat
)

// QRState is where a QR login is.
type QRState int

const (
	QRWaiting   QRState = iota // for the code to be scanned
	QRScanned                  // and for the login to be confirmed on the phone
	QRExpired                  // the code: a new one is needed
	QRConfirmed                // signed in
)

// A QRLogin is a QR code login under way.
type QRLogin struct {
	// Image is the code, PNG or JPEG, to show.
	Image []byte
	kind  LoginKind
	guid  string
	poll  func() (QRState, error)
	close func()
}

// StartQR starts a login with a code to scan with kind's app.
func StartQR(kind LoginKind) (*QRLogin, error) {
	q := &QRLogin{kind: kind, guid: device(), close: func() {}}
	var err error
	switch kind {
	case LoginQQ:
		err = q.startQQ()
	case LoginWeChat:
		err = q.startWeChat()
	default:
		err = q.startQQMusic()
	}
	if err != nil {
		return nil, err
	}
	return q, nil
}

// Poll waits a little for the login to move on, and returns where it is.
// Once confirmed, the listener is signed in.
func (q *QRLogin) Poll() (QRState, error) { return q.poll() }

// Close ends the login, as its dialog closes.
func (q *QRLogin) Close() { q.close() }

// signIn takes up cred, filling in what of it the app needs.
func signIn(cred *Credential) {
	useSession(withEncryptedUIN(cred))
}

// The QQ Music app's code: its scanning comes over MQTT (see mqtt.go).

func (q *QRLogin) startQQMusic() error {
	res, err := loginCall(nil, q.guid, "music.login.LoginServer", "CreateQRCode", obj{"tmeAppID": "qqmusic", "ct": 19, "cv": 2201}, nil)
	if err != nil {
		return err
	}
	d := get(res, "result", "data")
	code, id := str(d, "qrcode"), str(d, "qrcodeID")
	png, ok := strings.CutPrefix(code, "data:image/png;base64,")
	if !ok || id == "" {
		return errors.New("QQ 音乐返回了无法识别的二维码")
	}
	q.Image, err = base64.StdEncoding.DecodeString(png)
	if err != nil {
		return err
	}
	m := newMQTTLogin(id)
	q.close = m.close
	q.poll = func() (QRState, error) {
		ev, err := m.poll()
		switch {
		case err != nil:
			return QRWaiting, err
		case ev.kind == mqttCookies:
			res, err := loginCall(nil, q.guid, "music.login.LoginServer", "Login",
				obj{"musicid": ev.musicID, "qrCodeID": id, "token": ev.musicKey}, obj{"tmeLoginType": 6})
			if err != nil {
				return QRWaiting, err
			}
			cred, err := credential(res, q.guid)
			if err != nil {
				return QRWaiting, err
			}
			signIn(cred)
			return QRConfirmed, nil
		case ev.kind == mqttScanned:
			return QRScanned, nil
		case ev.kind == mqttExpired:
			return QRExpired, nil
		case ev.kind == mqttCanceled:
			return QRWaiting, errors.New("已取消登录")
		case ev.kind == mqttFailed:
			return QRWaiting, errors.New("登录失败，请重新扫码")
		}
		return QRWaiting, nil
	}
	return nil
}

// QQ's code, through QQ's own login (ptlogin), which authorizes QQ Music.

const (
	qqAppID       = "716027609"
	qqMusicAppID  = "100497308"
	qqLoginJump   = "https://graph.qq.com/oauth2.0/login_jump"
	qqLoginRefer  = "https://xui.ptlogin2.qq.com/cgi-bin/xlogin?appid=716027609&daid=383&style=33&target=self&pt_3rd_aid=100497308&s_url=https%3A%2F%2Fgraph.qq.com%2Foauth2.0%2Flogin_jump"
	musicRedirect = "https://y.qq.com/portal/wx_redirect.html?login_type=1&surl=https://y.qq.com/"
)

// ptClient is a client of its own cookies, which the steps of a QQ login
// pass on, and which follows no redirects: where they go is the answer.
func ptClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar:           jar,
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func cookieOf(c *http.Client, u, name string) string {
	parsed, _ := url.Parse(u)
	for _, ck := range c.Jar.Cookies(parsed) {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}

func ptGet(c *http.Client, u string, q url.Values) (*http.Response, error) {
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", qqLoginRefer)
	req.Header.Set("User-Agent", UserAgent)
	return c.Do(req)
}

func (q *QRLogin) startQQ() error {
	c := ptClient()
	res, err := ptGet(c, "https://ssl.ptlogin2.qq.com/ptqrshow", url.Values{
		"appid": {qqAppID}, "e": {"2"}, "l": {"M"}, "s": {"3"}, "d": {"72"}, "v": {"4"},
		"t": {strconv.FormatFloat(rand.Float64(), 'f', -1, 64)}, "daid": {"383"}, "pt_3rd_aid": {qqMusicAppID},
	})
	if err != nil {
		return err
	}
	q.Image, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return err
	}
	qrsig := cookieOf(c, "https://ssl.ptlogin2.qq.com/ptqrlogin", "qrsig")
	if qrsig == "" {
		return errors.New("QQ 未返回二维码会话")
	}
	// The code's token, which hashes from 0, as g_tk does from 5381.
	var token uint32
	for _, b := range []byte(qrsig) {
		token = token*33 + uint32(b)
	}
	token &= 0x7fffffff
	q.poll = func() (QRState, error) {
		time.Sleep(time.Second)
		res, err := ptGet(c, "https://ssl.ptlogin2.qq.com/ptqrlogin", url.Values{
			"u1": {qqLoginJump}, "ptqrtoken": {fmt.Sprint(token)}, "ptredirect": {"0"}, "h": {"1"}, "t": {"1"}, "g": {"1"},
			"from_ui": {"1"}, "ptlang": {"2052"}, "action": {fmt.Sprintf("0-0-%d", time.Now().UnixMilli())},
			"js_ver": {"20102616"}, "js_type": {"1"}, "pt_uistyle": {"40"}, "aid": {qqAppID}, "daid": {"383"},
			"pt_3rd_aid": {qqMusicAppID}, "has_onekey": {"1"},
		})
		if err != nil {
			return QRWaiting, err
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		status, redirect, err := parsePtStatus(string(b))
		if err != nil {
			return QRWaiting, err
		}
		switch status {
		case 66:
			return QRWaiting, nil
		case 67:
			return QRScanned, nil
		case 65:
			return QRExpired, nil
		case 68:
			return QRWaiting, errors.New("已取消 QQ 登录")
		case 0:
			cred, err := finishQQ(c, redirect, q.guid)
			if err != nil {
				return QRWaiting, err
			}
			signIn(cred)
			return QRConfirmed, nil
		}
		return QRWaiting, fmt.Errorf("QQ 扫码返回未知状态：%d", status)
	}
	return nil
}

// parsePtStatus reads ptlogin's answer, ptuiCB('66','0',”,…): its status,
// and once signed in, where to go on to.
func parsePtStatus(body string) (int, string, error) {
	s := strings.TrimSuffix(strings.TrimSpace(body), ";")
	s, ok := strings.CutPrefix(s, "ptuiCB('")
	if !ok {
		return 0, "", errors.New("QQ 扫码响应格式无效")
	}
	s, ok = strings.CutSuffix(s, "')")
	if !ok {
		return 0, "", errors.New("QQ 扫码响应格式无效")
	}
	args := strings.Split(s, "','")
	status, err := strconv.Atoi(args[0])
	if err != nil {
		return 0, "", errors.New("QQ 扫码响应格式无效")
	}
	if status != 0 {
		return status, "", nil
	}
	if len(args) < 3 || !validCheckSig(args[2]) {
		return 0, "", errors.New("QQ 返回了无效的授权地址")
	}
	return 0, args[2], nil
}

func validCheckSig(u string) bool {
	p, err := url.Parse(u)
	return err == nil && p.Scheme == "https" && p.Host == "ssl.ptlogin2.graph.qq.com" && p.Path == "/check_sig"
}

// finishQQ follows ptlogin's redirect, which gives the session, then
// authorizes QQ Music with it, and trades the code it gives for a login.
func finishQQ(c *http.Client, redirect, guid string) (*Credential, error) {
	res, err := ptGet(c, redirect, nil)
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	pskey := cookieOf(c, "https://graph.qq.com/oauth2.0/authorize", "p_skey")
	if pskey == "" {
		return nil, errors.New("QQ 授权会话缺少 p_skey，请重新扫码")
	}
	form := url.Values{
		"response_type": {"code"}, "client_id": {qqMusicAppID}, "redirect_uri": {musicRedirect},
		"scope": {"get_user_info,get_app_friends"}, "state": {"state"}, "switch": {""}, "from_ptlogin": {"1"},
		"src": {"1"}, "update_auth": {"1"}, "openapi": {"1010_1030"}, "g_tk": {fmt.Sprint(hash33(pskey))},
		"auth_time": {fmt.Sprint(time.Now().UnixMilli())}, "ui": {newUUID()},
	}
	req, err := http.NewRequest("POST", "https://graph.qq.com/oauth2.0/authorize", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://graph.qq.com/")
	req.Header.Set("User-Agent", UserAgent)
	res, err = c.Do(req)
	if err != nil {
		return nil, err
	}
	res.Body.Close()
	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil || loc.Host != "y.qq.com" || loc.Path != "/portal/wx_redirect.html" {
		return nil, errors.New("QQ 音乐授权未完成，请重新扫码")
	}
	code := loc.Query().Get("code")
	if code == "" {
		return nil, errors.New("QQ 未返回音乐授权码")
	}
	r, err := loginCall(nil, guid, "QQConnectLogin.LoginServer", "QQLogin",
		obj{"onlyNeedAccessToken": 0, "forceRefreshToken": 0, "appid": 100497308, "code": code},
		obj{"ct": 19, "cv": 1, "tmeLoginType": 2})
	if err != nil {
		return nil, err
	}
	return credential(r, guid)
}

func newUUID() string {
	g := strings.ToLower(newGUID())
	return g[:8] + "-" + g[8:12] + "-" + g[12:16] + "-" + g[16:20] + "-" + g[20:]
}

// WeChat's code, through WeChat's web login, which long-polls.

const wechatAppID = "wx48db31d50e334801"

func (q *QRLogin) startWeChat() error {
	page, err := http.NewRequest("GET", "https://open.weixin.qq.com/connect/qrconnect?"+url.Values{
		"appid": {wechatAppID}, "redirect_uri": {"https://y.qq.com/portal/wx_redirect.html?login_type=2&surl=https://y.qq.com/"},
		"response_type": {"code"}, "scope": {"snsapi_login"}, "state": {"STATE"},
		"href": {"https://y.qq.com/mediastyle/music_v17/src/css/popup_wechat.css#wechat_redirect"},
	}.Encode(), nil)
	if err != nil {
		return err
	}
	page.Header.Set("User-Agent", UserAgent)
	res, err := httpClient.Do(page)
	if err != nil {
		return err
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	_, tail, _ := strings.Cut(string(b), "uuid=")
	uuid, _, _ := strings.Cut(tail, `"`)
	if uuid == "" || strings.ContainsFunc(uuid, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_-=", r))
	}) {
		return errors.New("微信登录页面缺少二维码标识")
	}
	img, err := http.NewRequest("GET", "https://open.weixin.qq.com/connect/qrcode/"+uuid, nil)
	if err != nil {
		return err
	}
	img.Header.Set("Referer", "https://open.weixin.qq.com/connect/qrconnect")
	img.Header.Set("User-Agent", UserAgent)
	res, err = httpClient.Do(img)
	if err != nil {
		return err
	}
	q.Image, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return err
	}
	long := &http.Client{Timeout: 40 * time.Second}
	q.poll = func() (QRState, error) {
		req, err := http.NewRequest("GET", "https://lp.open.weixin.qq.com/connect/l/qrconnect?"+url.Values{
			"uuid": {uuid}, "_": {fmt.Sprint(time.Now().UnixMilli())}}.Encode(), nil)
		if err != nil {
			return QRWaiting, err
		}
		req.Header.Set("Referer", "https://open.weixin.qq.com/")
		req.Header.Set("User-Agent", UserAgent)
		res, err := long.Do(req)
		if err != nil {
			// A long poll timing out is no failure.
			return QRWaiting, nil
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		status, code, err := parseWeChatStatus(string(b))
		if err != nil {
			return QRWaiting, err
		}
		switch status {
		case 408:
			return QRWaiting, nil
		case 404:
			return QRScanned, nil
		case 402:
			return QRExpired, nil
		case 403:
			return QRWaiting, errors.New("已取消微信登录")
		case 405:
			if code == "" {
				return QRWaiting, errors.New("微信确认登录后未返回授权码")
			}
			// Its page says login_type=2, but QQ Music takes WeChat as 1.
			r, err := loginCall(nil, q.guid, "music.login.LoginServer", "Login",
				obj{"code": code, "strAppid": wechatAppID}, obj{"tmeLoginType": 1})
			if err != nil {
				return QRWaiting, err
			}
			cred, err := credential(r, q.guid)
			if err != nil {
				return QRWaiting, err
			}
			signIn(cred)
			return QRConfirmed, nil
		}
		return QRWaiting, fmt.Errorf("微信扫码返回未知状态：%d", status)
	}
	return nil
}

// parseWeChatStatus reads window.wx_errcode=408;window.wx_code=”;.
func parseWeChatStatus(body string) (int, string, error) {
	_, tail, ok := strings.Cut(body, "window.wx_errcode=")
	if !ok {
		return 0, "", errors.New("微信扫码响应缺少状态码")
	}
	head, tail, _ := strings.Cut(tail, ";")
	status, err := strconv.Atoi(strings.TrimSpace(head))
	if err != nil {
		return 0, "", errors.New("微信扫码状态码无效")
	}
	_, tail, ok = strings.Cut(tail, "window.wx_code='")
	code := ""
	if ok {
		code, _, _ = strings.Cut(tail, "'")
	}
	return status, code, nil
}
