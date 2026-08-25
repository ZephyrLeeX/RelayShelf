package auth

import (
	"net/http"
	"net/url"
	"time"
)

type CookiePolicy struct {
	Name   string
	Secure bool
}

func NewCookiePolicy(origin *url.URL) CookiePolicy {
	if origin.Scheme == "https" {
		return CookiePolicy{Name: "__Host-session", Secure: true}
	}
	return CookiePolicy{Name: "relayshelf_session", Secure: false}
}
func (p CookiePolicy) Set(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: p.Name, Value: token, Path: "/", Expires: expires, HttpOnly: true, Secure: p.Secure, SameSite: http.SameSiteLaxMode})
}
func (p CookiePolicy) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: p.Name, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: p.Secure, SameSite: http.SameSiteLaxMode})
}
