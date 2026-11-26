// Package auth tells the owner of the archive from a visitor. The owner logs in at /login with the password they set
// on the server (atrace-server set-password), and gets a signed cookie; everyone else is a visitor, who sees only the
// entries the owner made public. The owner changes the password at PUT /admin/password. The collectors, which write
// to /collect, show the token in ATRACE_COLLECT_TOKEN; the CLI sends the owner's password as a bearer token.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"git.zexi.me/zexi/atrace/server/core"
)

const (
	cookieName = "atrace_session"
	sessionAge = 30 * 24 * time.Hour
	// A run of wrong passwords is slowed to maxFailures per failureWindow, for the whole server.
	maxFailures   = 10
	failureWindow = time.Minute
	// iterations is how hard the password is to guess from its verifier; the CLI pays it on each request it makes.
	iterations = 600_000
	// MinPassword is the shortest password the owner may set, in characters.
	MinPassword = 8
)

// ErrShortPassword is what SetPassword answers for a password shorter than MinPassword.
var ErrShortPassword = fmt.Errorf("密码至少 %d 个字符", MinPassword)

type ownerKey struct{}

// Visitor is the context of a request seen as a visitor's, whoever made it: for what is for visitors only.
func Visitor(ctx context.Context) context.Context {
	return context.WithValue(ctx, ownerKey{}, false)
}

// Owner reports whether the request came from the logged-in owner.
func Owner(ctx context.Context) bool {
	owner, _ := ctx.Value(ownerKey{}).(bool)
	return owner
}

type Auth struct {
	store *core.Store
	// collectToken is what a collector sends as a bearer token; without one nothing can write.
	collectToken string

	mu       sync.Mutex
	failures []time.Time
}

// New makes the auth of the archive in the store. The owner's password is in the store, not here; until one is set
// nobody can log in, so everyone is a visitor.
func New(store *core.Store) *Auth {
	a := &Auth{store: store, collectToken: os.Getenv("ATRACE_COLLECT_TOKEN")}
	if a.collectToken == "" {
		log.Printf("ATRACE_COLLECT_TOKEN is not set: /collect is closed to everyone")
	}
	return a
}

// SetPassword makes password the owner's, and signs the owner's cookies with a new key, so the cookies from before
// stop working.
func SetPassword(ctx context.Context, store *core.Store, password string) error {
	if utf8.RuneCountInString(password) < MinPassword {
		return ErrShortPassword
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	verifier, err := derive(password, salt)
	if err != nil {
		return err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	return store.SetOwnerLogin(ctx, core.Login{Salt: salt, Verifier: verifier, Key: key})
}

func derive(password string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, password, salt, iterations, 32)
}

// checkPassword reports the owner's login and whether password is the owner's. With no password set, nothing is.
func (a *Auth) checkPassword(ctx context.Context, password string) (core.Login, bool, error) {
	l, err := a.store.OwnerLogin(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return core.Login{}, false, nil
	}
	if err != nil {
		return core.Login{}, false, err
	}
	verifier, err := derive(password, l.Salt)
	if err != nil {
		return core.Login{}, false, err
	}
	return l, subtle.ConstantTimeCompare(verifier, l.Verifier) == 1, nil
}

// collector reports whether the request carries the collect token. It is compared by digest, so
// the check takes the same time whatever the token's length, and it holds for every address:
// a proxy on this machine also connects from 127.0.0.1.
func (a *Auth) collector(r *http.Request) bool {
	return bearerIs(r, a.collectToken)
}

// bearerIs reports whether the request carries the token as a bearer token; no token is never carried.
func bearerIs(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	given, ok := bearer(r)
	if !ok {
		return false
	}
	want, got := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(given))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

func bearer(r *http.Request) (string, bool) {
	return strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func sign(key []byte, expires int64) string {
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "%d", expires)
	return strconv.FormatInt(expires, 10) + "." + hex.EncodeToString(mac.Sum(nil))
}

func valid(key []byte, token string) bool {
	expires, sig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	at, err := strconv.ParseInt(expires, 10, 64)
	if err != nil || time.Now().Unix() > at {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(sign(key, at))) == 1 && sig != ""
}

// isOwner reports whether the request carries a login cookie the owner's key signed.
func (a *Auth) isOwner(ctx context.Context, r *http.Request) (bool, error) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false, nil
	}
	l, err := a.store.OwnerLogin(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return valid(l.Key, c.Value), nil
}

// ownerBearer reports whether the request carries the owner's password as a bearer token.
func (a *Auth) ownerBearer(ctx context.Context, r *http.Request) (bool, error) {
	password, ok := bearer(r)
	if !ok || password == "" {
		return false, nil
	}
	_, ok, err := a.checkPassword(ctx, password)
	return ok, err
}

// AsOwner marks every request as the owner's, for tests of what the owner sees.
func AsOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerKey{}, true)))
	})
}

// Middleware marks each request owner or visitor, and keeps visitors out of the paths
// that read everything or write: /admin. /collect is for the collectors alone.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/collect/") {
			if !a.collector(r) {
				http.Error(w, "collect token required", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerKey{}, false)))
			return
		}
		owner, err := a.isOwner(r.Context(), r)
		if err == nil && !owner {
			owner, err = a.ownerBearer(r.Context(), r)
		}
		if err != nil {
			log.Printf("auth: %v", err)
			http.Error(w, "服务端出错", http.StatusInternalServerError)
			return
		}
		if !owner && strings.HasPrefix(r.URL.Path, "/admin/") {
			http.Error(w, "login required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerKey{}, owner)))
	})
}

// Register adds the login, logout and password endpoints.
func (a *Auth) Register(m *http.ServeMux) {
	m.HandleFunc("POST /login", a.login)
	m.HandleFunc("PUT /admin/password", a.changePassword)
	m.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
}

func (a *Auth) throttled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	cutoff := time.Now().Add(-failureWindow)
	kept := a.failures[:0]
	for _, at := range a.failures {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	a.failures = kept
	return len(a.failures) >= maxFailures
}

func (a *Auth) fail() {
	a.mu.Lock()
	a.failures = append(a.failures, time.Now())
	a.mu.Unlock()
}

// setCookie logs the browser in as the owner, with the owner's current key.
func setCookie(w http.ResponseWriter, r *http.Request, l core.Login) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: sign(l.Key, time.Now().Add(sessionAge).Unix()), Path: "/",
		MaxAge: int(sessionAge.Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, err := a.store.OwnerLogin(r.Context()); errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "还没有设置密码：请在服务器上运行 atrace-server set-password", http.StatusServiceUnavailable)
		return
	}
	if a.throttled() {
		http.Error(w, "尝试太频繁，稍后再试", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "请求无效", http.StatusBadRequest)
		return
	}
	l, ok, err := a.checkPassword(r.Context(), r.PostForm.Get("password"))
	if err != nil {
		http.Error(w, "服务端出错", http.StatusInternalServerError)
		return
	}
	if !ok {
		a.fail()
		time.Sleep(500 * time.Millisecond)
		http.Error(w, "密码不对", http.StatusUnauthorized)
		return
	}
	setCookie(w, r, l)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// changePassword sets the owner's password to the new one, given the current one. The browser that asks stays logged in.
func (a *Auth) changePassword(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if a.throttled() {
		http.Error(w, "尝试太频繁，稍后再试", http.StatusTooManyRequests)
		return
	}
	var req struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
	if err != nil || json.Unmarshal(body, &req) != nil {
		http.Error(w, "请求无效", http.StatusBadRequest)
		return
	}
	_, ok, err := a.checkPassword(r.Context(), req.Current)
	if err != nil {
		http.Error(w, "服务端出错", http.StatusInternalServerError)
		return
	}
	if !ok {
		a.fail()
		time.Sleep(500 * time.Millisecond)
		http.Error(w, "当前密码不对", http.StatusUnauthorized)
		return
	}
	if err := SetPassword(r.Context(), a.store, req.Password); errors.Is(err, ErrShortPassword) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if err != nil {
		http.Error(w, "服务端出错", http.StatusInternalServerError)
		return
	}
	l, err := a.store.OwnerLogin(r.Context())
	if err != nil {
		http.Error(w, "服务端出错", http.StatusInternalServerError)
		return
	}
	setCookie(w, r, l)
	w.WriteHeader(http.StatusNoContent)
}
