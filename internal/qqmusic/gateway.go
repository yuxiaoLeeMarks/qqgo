package qqmusic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// The desktop client asks QQ Music's gateway for everything: a request
// names a module and a method of it, with their parameters, and carries
// who is asking in its "comm", and in a cookie once signed in. Its body is
// signed, as lyrune found the client does.

const gatewayURL = "https://u.y.qq.com/cgi-bin/musics.fcg"

// UserAgent is the browser the app's requests say they come from.
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var httpClient = &http.Client{Timeout: 20 * time.Second}

// APIError is a request the gateway refused, with its code.
type APIError struct {
	Method string
	Code   int64
}

func (e *APIError) Error() string { return fmt.Sprintf("qqmusic: %s: code %d", e.Method, e.Code) }

// ErrSignedOut is what a request needing the listener gives without them,
// or once QQ Music no longer takes their login.
var ErrSignedOut = errors.New("qqmusic: not signed in")

// rejected reports whether code says the login is no longer good.
func rejected(code int64) bool { return code == 1000 || code == 104400 || code == 104401 }

// marshal writes v as the client does: without escaping <, > and &, which
// the server would sign as written.
func marshal(v any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	e.Encode(v)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

// comm is who asks: the listener signed in, as cred, or nobody.
func comm(cred *Credential) obj {
	c := obj{"ct": 19, "cv": 2201, "chid": "0", "uin": "0", "g_tk": hash33(""), "guid": device()}
	if cred != nil {
		c["uin"] = fmt.Sprint(cred.MusicID)
		c["g_tk"] = hash33(cred.MusicKey)
		c["guid"] = cred.guid()
		c["tmeLoginType"] = cred.LoginType
	}
	return c
}

// call asks the gateway for module.method with param, as the listener if
// signed in, and returns its data.
func call(module, method string, param any) (any, error) {
	cred, err := fresh()
	if err != nil && !errors.Is(err, ErrSignedOut) {
		return nil, err
	}
	return callAs(cred, module, method, param, nil)
}

// callSignedIn is call for what only the listener may ask.
func callSignedIn(module, method string, param any) (any, error) {
	cred, err := fresh()
	if err != nil {
		return nil, err
	}
	return callAs(cred, module, method, param, nil)
}

// callAs asks as cred, or nobody, with what of comm extra changes.
func callAs(cred *Credential, module, method string, param any, extra obj) (any, error) {
	c := comm(cred)
	for k, v := range extra {
		c[k] = v
	}
	body := marshal(obj{"comm": c, "req": obj{"module": module, "method": method, "param": param}})
	q := url.Values{"sign": {sign(body)}}
	req, err := http.NewRequest("POST", gatewayURL+"?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://y.qq.com/")
	req.Header.Set("Origin", "https://y.qq.com")
	if cred != nil {
		req.Header.Set("Cookie", cred.cookie())
	}
	res, err := send(req)
	if err != nil {
		return nil, err
	}
	return unwrap(res, "req", module+"."+method, cred)
}

// newGet makes a GET of u as y.qq.com's pages make it.
func newGet(u string) (*http.Request, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://y.qq.com/")
	return req, nil
}

// send sends req and reads its answer as JSON.
func send(req *http.Request) (any, error) {
	req.Header.Set("User-Agent", UserAgent)
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("qqmusic: %s: %s", req.URL.Path, res.Status)
	}
	v, err := decode(b)
	if err != nil {
		return nil, fmt.Errorf("qqmusic: %s: %w", req.URL.Path, err)
	}
	return v, nil
}

// unwrap returns the data of the request named key in res, or its error:
// a login refused signs the listener out.
func unwrap(res any, key, method string, cred *Credential) (any, error) {
	check := func(code int64) error {
		if code == 0 {
			return nil
		}
		if cred != nil && rejected(code) {
			revoke(cred)
			return ErrSignedOut
		}
		return &APIError{Method: method, Code: code}
	}
	if err := check(num(res, "code")); err != nil {
		return nil, err
	}
	r := get(res, key)
	if r == nil {
		return nil, fmt.Errorf("qqmusic: %s: no answer", method)
	}
	if err := check(num(r, "code")); err != nil {
		return nil, err
	}
	return get(r, "data"), nil
}
