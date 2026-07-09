package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Dev-mode auth stub per Phase-1 contract C7: POST /api/v1/auth/login
// validates against the seeded managers table and issues JWTs signed with
// HIKRAD_JWT_SECRET. The route registers only after EnableDevLogin, which
// main calls only when HIKRAD_ENV=dev; Phase 2 (Agent 1) replaces the
// internals with the same request/response shape.

// Token type claim values.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"

	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 7 * 24 * time.Hour
)

var devLoginSecret []byte

// EnableDevLogin arms the C7 dev login stub with the JWT signing secret.
// Like SetAuthenticator, it is an injection seam called from main before
// NewRouter — never in prod.
func EnableDevLogin(jwtSecret []byte) { devLoginSecret = jwtSecret }

type devAuthModule struct{}

func (devAuthModule) Name() string { return "auth-dev-stub" }

func (devAuthModule) Register(r chi.Router, d Deps) {
	if devLoginSecret == nil {
		return
	}
	r.Post("/api/v1/auth/login", loginHandler(d))
}

func init() { Add(devAuthModule{}) }

type loginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

type loginManager struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type loginResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	Manager      loginManager `json:"manager"`
}

type managerRecord struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string
}

// lookupManager is a package variable so unit tests can stub the database.
var lookupManager = func(ctx context.Context, db *pgxpool.Pool, username string) (*managerRecord, error) {
	var m managerRecord
	err := db.QueryRow(ctx,
		`SELECT id::text, username, password_hash, role FROM managers WHERE username = $1`,
		username,
	).Scan(&m.ID, &m.Username, &m.PasswordHash, &m.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func loginHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if !Bind(w, r, &req) {
			return
		}
		m, err := lookupManager(r.Context(), d.DB, req.Username)
		if err != nil {
			d.Log.Error("login lookup failed", "error", err)
			Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		if m == nil || bcrypt.CompareHashAndPassword([]byte(m.PasswordHash), []byte(req.Password)) != nil {
			Error(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
			return
		}
		access, refresh, err := IssueTokens(devLoginSecret, m.ID, m.Role, time.Now())
		if err != nil {
			d.Log.Error("token signing failed", "error", err)
			Error(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		JSON(w, http.StatusOK, loginResponse{
			AccessToken:  access,
			RefreshToken: refresh,
			Manager:      loginManager{ID: m.ID, Username: m.Username, Role: m.Role},
		})
	}
}

// IssueTokens signs the access/refresh JWT pair (claims: sub = manager id,
// role, typ, iat, exp) with the given HMAC secret.
func IssueTokens(secret []byte, managerID, role string, now time.Time) (access, refresh string, err error) {
	sign := func(typ string, ttl time.Duration) (string, error) {
		t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":  managerID,
			"role": role,
			"typ":  typ,
			"iat":  now.Unix(),
			"exp":  now.Add(ttl).Unix(),
		})
		return t.SignedString(secret)
	}
	if access, err = sign(TokenTypeAccess, accessTokenTTL); err != nil {
		return "", "", err
	}
	if refresh, err = sign(TokenTypeRefresh, refreshTokenTTL); err != nil {
		return "", "", err
	}
	return access, refresh, nil
}
