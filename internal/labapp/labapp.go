// Package labapp is the intentionally small practice target: login, identity,
// document list and document read, in a vulnerable or fixed access-control
// mode. It holds only synthetic data.
//
// Only cmd/labapp and the evaluator (tests) may import this package. The agent
// must never import it, and no HTTP response reveals the mode or the answer key.
package labapp

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	_ "modernc.org/sqlite"
)

type Mode string

const (
	ModeVulnerable Mode = "vulnerable" // any logged-in user can read any document
	ModeFixed      Mode = "fixed"      // only the owner can read a document
)

type Credential struct {
	UserID      string
	DisplayName string
	Username    string
	Password    string
}

type Config struct {
	Mode   Mode
	Seed   string
	DBPath string // empty = in-memory
	Users  []Credential
}

type App struct {
	mode  Mode
	db    *sql.DB
	users []Credential

	mu       sync.Mutex
	sessions map[string]string // token -> user id
	truth    []DocTruth
}

// DocTruth and Answer are evaluator-only; nothing here is served over HTTP.
type DocTruth struct {
	ID, OwnerID, Marker string
}

type Answer struct {
	Mode Mode
	Docs []DocTruth
	// CrossAccessExpected is true when a user can read another user's document.
	CrossAccessExpected bool
}

func New(cfg Config) (*App, error) {
	if cfg.Mode != ModeVulnerable && cfg.Mode != ModeFixed {
		return nil, fmt.Errorf("labapp: invalid mode %q", cfg.Mode)
	}
	if len(cfg.Users) != 2 {
		return nil, fmt.Errorf("labapp: exactly two users required")
	}
	for _, u := range cfg.Users {
		if u.UserID == "" || u.Username == "" || u.Password == "" {
			return nil, fmt.Errorf("labapp: user %q has empty field", u.UserID)
		}
	}
	dsn := cfg.DBPath
	if dsn == "" {
		dsn = ":memory:"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // keeps :memory: a single database
	a := &App{mode: cfg.Mode, db: db, users: cfg.Users, sessions: map[string]string{}}
	if err := a.seed(cfg.Seed); err != nil {
		db.Close()
		return nil, err
	}
	return a, nil
}

func (a *App) Close() error { return a.db.Close() }

func (a *App) Answer() Answer {
	return Answer{Mode: a.mode, Docs: append([]DocTruth(nil), a.truth...), CrossAccessExpected: a.mode == ModeVulnerable}
}

// Deterministic IDs and markers derived from the seed, so runs are reproducible.
func derive(seed string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(seed))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (a *App) seed(seed string) error {
	if seed == "" {
		seed = "gosumdochi-lab-1"
	}
	if _, err := a.db.Exec(`
DROP TABLE IF EXISTS documents;
CREATE TABLE documents (id TEXT PRIMARY KEY, owner_id TEXT NOT NULL, title TEXT NOT NULL, body TEXT NOT NULL);`); err != nil {
		return err
	}
	a.truth = nil
	for _, u := range a.users {
		for n := 1; n <= 2; n++ {
			id := "d-" + derive(seed, "doc", u.UserID, fmt.Sprint(n))[:8]
			marker := "LAB-MARK-" + derive(seed, "marker", id)[:12]
			title := fmt.Sprintf("%s note %d", u.DisplayName, n)
			body := fmt.Sprintf("Synthetic content for %s. %s", u.DisplayName, marker)
			if _, err := a.db.Exec(`INSERT INTO documents(id, owner_id, title, body) VALUES (?,?,?,?)`, id, u.UserID, title, body); err != nil {
				return err
			}
			a.truth = append(a.truth, DocTruth{ID: id, OwnerID: u.UserID, Marker: marker})
		}
	}
	return nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("GET /api/me", a.auth(a.me))
	mux.HandleFunc("GET /api/docs", a.auth(a.listDocs))
	mux.HandleFunc("GET /api/docs/{id}", a.auth(a.getDoc))
	return noStore(mux)
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!doctype html><title>Lab Notes</title><h1>Lab Notes</h1><p>Synthetic practice app.</p><a href="/api/docs">My documents</a>`))
}

const cookieName = "lab_session"

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	var user *Credential
	for i := range a.users {
		u := &a.users[i]
		okU := subtle.ConstantTimeCompare([]byte(in.Username), []byte(u.Username)) == 1
		okP := subtle.ConstantTimeCompare([]byte(in.Password), []byte(u.Password)) == 1
		if okU && okP {
			user = u
		}
	}
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	token := hex.EncodeToString(buf)
	a.mu.Lock()
	a.sessions[token] = user.UserID
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type authedHandler func(w http.ResponseWriter, r *http.Request, user Credential)

func (a *App) auth(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "login required"})
			return
		}
		a.mu.Lock()
		uid, ok := a.sessions[c.Value]
		a.mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "login required"})
			return
		}
		for _, u := range a.users {
			if u.UserID == uid {
				next(w, r, u)
				return
			}
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "login required"})
	}
}

func (a *App) me(w http.ResponseWriter, r *http.Request, u Credential) {
	writeJSON(w, http.StatusOK, map[string]string{"user_id": u.UserID, "display_name": u.DisplayName})
}

func (a *App) listDocs(w http.ResponseWriter, r *http.Request, u Credential) {
	rows, err := a.db.Query(`SELECT id, title FROM documents WHERE owner_id = ? ORDER BY id`, u.UserID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	defer rows.Close()
	type item struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		URL   string `json:"url"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Title); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
			return
		}
		it.URL = "/api/docs/" + it.ID
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": out})
}

func (a *App) getDoc(w http.ResponseWriter, r *http.Request, u Credential) {
	id := r.PathValue("id")
	var ownerID, title, body string
	err := a.db.QueryRow(`SELECT owner_id, title, body FROM documents WHERE id = ?`, id).Scan(&ownerID, &title, &body)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		return
	}
	// The access-control check under test.
	if a.mode == ModeFixed && ownerID != u.UserID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	ownerName := ownerID
	for _, c := range a.users {
		if c.UserID == ownerID {
			ownerName = c.DisplayName
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "owner_id": ownerID, "owner_name": ownerName, "title": title, "body": body})
}
