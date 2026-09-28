package api

import (
	"crypto/rand"
	"encoding/hex"
	"medtrust-raft/internal/store"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type authUser struct {
	Username     string `json:"username"`
	Role         string `json:"role"`
	PasswordHash string `json:"-"`
	RealName     string `json:"real_name,omitempty"`
	LicenseNo    string `json:"license_no,omitempty"`
	Hospital     string `json:"hospital,omitempty"`
	Department   string `json:"department,omitempty"`
	Verified     bool   `json:"verified"`
	VerifiedAt   string `json:"verified_at,omitempty"`
}

func (srv *AppServer) authRoutes(api *gin.RouterGroup) {
	api.POST("/auth/register", srv.handleRegister)
	api.POST("/auth/login", srv.handleLogin)
	api.POST("/auth/logout", srv.handleLogout)
	api.GET("/auth/me", srv.handleMe)
	api.GET("/auth/users", srv.authRequired("admin"), srv.handleUsers)
	api.POST("/auth/doctors", srv.authRequired("admin"), srv.handleCreateDoctor)
	api.PUT("/auth/doctors/:username/verify", srv.authRequired("admin"), srv.handleVerifyDoctor)
}

func (srv *AppServer) authRequired(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if token == "" {
			token, _ = c.Cookie("medtrust_session")
		}
		srv.authMu.Lock()
		user, ok := srv.sessions[token]
		srv.authMu.Unlock()
		if !ok || token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "login required"})
			c.Abort()
			return
		}
		if len(roles) > 0 {
			allowed := false
			for _, role := range roles {
				if user.Role == role {
					allowed = true
				}
			}
			if !allowed {
				c.JSON(http.StatusForbidden, gin.H{"error": "insufficient role"})
				c.Abort()
				return
			}
		}
		c.Set("auth_user", user)
		c.Next()
	}
}

func (srv *AppServer) handleRegister(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&req) != nil || len(strings.TrimSpace(req.Username)) < 3 || !passwordIsAcceptable(req.Password) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username must be 3+ chars and password must be 12-128 chars"})
		return
	}
	name := strings.TrimSpace(req.Username)
	hash, err := hashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to secure password"})
		return
	}
	srv.authMu.Lock()
	defer srv.authMu.Unlock()
	if _, exists := srv.users[name]; exists {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}
	srv.users[name] = authUser{Username: name, Role: "patient", PasswordHash: hash}
	if err := srv.store.SavePortalUser(store.PortalUser{Username: name, Role: "patient", PasswordHash: hash}); err != nil {
		delete(srv.users, name)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save account"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"username": name, "role": "patient"})
}

func (srv *AppServer) handleLogin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	srv.authMu.Lock()
	user, ok := srv.users[strings.TrimSpace(req.Username)]
	srv.authMu.Unlock()
	valid, needsUpgrade := false, false
	if ok {
		valid, needsUpgrade = verifyPassword(user.PasswordHash, req.Password)
	}
	if !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}
	if needsUpgrade {
		upgradedHash, err := hashPassword(req.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upgrade password security"})
			return
		}
		user.PasswordHash = upgradedHash
		if err := srv.store.SavePortalUser(portalUserForStore(user)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to upgrade password security"})
			return
		}
		srv.authMu.Lock()
		srv.users[user.Username] = user
		srv.authMu.Unlock()
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session unavailable"})
		return
	}
	token := hex.EncodeToString(b)
	srv.authMu.Lock()
	srv.sessions[token] = user
	srv.authMu.Unlock()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("medtrust_session", token, 8*60*60, "/", "", false, true)
	c.JSON(http.StatusOK, publicAuthUser(user, true))
}

func (srv *AppServer) handleLogout(c *gin.Context) {
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if token == "" {
		token, _ = c.Cookie("medtrust_session")
	}
	srv.authMu.Lock()
	delete(srv.sessions, token)
	srv.authMu.Unlock()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("medtrust_session", "", -1, "/", "", false, true)
	c.Status(http.StatusNoContent)
}
func (srv *AppServer) handleMe(c *gin.Context) {
	token, _ := c.Cookie("medtrust_session")
	srv.authMu.Lock()
	user, ok := srv.sessions[token]
	srv.authMu.Unlock()
	if !ok || token == "" {
		c.JSON(http.StatusOK, gin.H{"authenticated": false})
		return
	}
	c.JSON(http.StatusOK, publicAuthUser(user, true))
}
func (srv *AppServer) handleUsers(c *gin.Context) {
	srv.authMu.Lock()
	defer srv.authMu.Unlock()
	out := make([]authUser, 0, len(srv.users))
	for _, u := range srv.users {
		out = append(out, u)
	}
	c.JSON(http.StatusOK, out)
}
func (srv *AppServer) handleCreateDoctor(c *gin.Context) {
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		RealName   string `json:"real_name"`
		LicenseNo  string `json:"license_no"`
		Hospital   string `json:"hospital"`
		Department string `json:"department"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid doctor account"})
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || !passwordIsAcceptable(req.Password) || strings.TrimSpace(req.RealName) == "" || strings.TrimSpace(req.LicenseNo) == "" || strings.TrimSpace(req.Hospital) == "" || strings.TrimSpace(req.Department) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "doctor identity information is required"})
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to secure password"})
		return
	}
	srv.authMu.Lock()
	defer srv.authMu.Unlock()
	if _, ok := srv.users[req.Username]; ok {
		c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
		return
	}
	verifiedAt := time.Now().Format(time.RFC3339)
	doctor := authUser{Username: req.Username, Role: "doctor", PasswordHash: hash, RealName: strings.TrimSpace(req.RealName), LicenseNo: strings.TrimSpace(req.LicenseNo), Hospital: strings.TrimSpace(req.Hospital), Department: strings.TrimSpace(req.Department), Verified: true, VerifiedAt: verifiedAt}
	srv.users[req.Username] = doctor
	if err := srv.store.SavePortalUser(portalUserForStore(doctor)); err != nil {
		delete(srv.users, req.Username)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save account"})
		return
	}
	c.JSON(http.StatusCreated, doctor)
}

func (srv *AppServer) handleVerifyDoctor(c *gin.Context) {
	var req struct {
		RealName   string `json:"real_name"`
		LicenseNo  string `json:"license_no"`
		Hospital   string `json:"hospital"`
		Department string `json:"department"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.RealName) == "" || strings.TrimSpace(req.LicenseNo) == "" || strings.TrimSpace(req.Hospital) == "" || strings.TrimSpace(req.Department) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "complete doctor identity information is required"})
		return
	}
	srv.authMu.Lock()
	defer srv.authMu.Unlock()
	doctor, ok := srv.users[c.Param("username")]
	if !ok || doctor.Role != "doctor" {
		c.JSON(http.StatusNotFound, gin.H{"error": "doctor not found"})
		return
	}
	doctor.RealName = strings.TrimSpace(req.RealName)
	doctor.LicenseNo = strings.TrimSpace(req.LicenseNo)
	doctor.Hospital = strings.TrimSpace(req.Hospital)
	doctor.Department = strings.TrimSpace(req.Department)
	doctor.Verified = true
	doctor.VerifiedAt = time.Now().Format(time.RFC3339)
	srv.users[doctor.Username] = doctor
	if err := srv.store.SavePortalUser(portalUserForStore(doctor)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save verification"})
		return
	}
	c.JSON(http.StatusOK, doctor)
}

func portalUserForStore(user authUser) store.PortalUser {
	return store.PortalUser{Username: user.Username, Role: user.Role, PasswordHash: user.PasswordHash, RealName: user.RealName, LicenseNo: user.LicenseNo, Hospital: user.Hospital, Department: user.Department, Verified: user.Verified, VerifiedAt: user.VerifiedAt}
}

func publicAuthUser(user authUser, authenticated bool) gin.H {
	return gin.H{"authenticated": authenticated, "username": user.Username, "role": user.Role, "real_name": user.RealName, "license_no": user.LicenseNo, "hospital": user.Hospital, "department": user.Department, "verified": user.Verified, "verified_at": user.VerifiedAt}
}
