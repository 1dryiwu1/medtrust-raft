package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"medtrust-raft/internal/store"

	"github.com/gin-gonic/gin"
)

func (srv *AppServer) consentRoutes(api *gin.RouterGroup) {
	api.GET("/portal/doctors", srv.authRequired("patient", "doctor", "admin"), srv.handleDoctors)
	api.GET("/portal/consents", srv.authRequired("patient", "doctor"), srv.handleConsents)
	api.POST("/portal/consents", srv.authRequired("patient"), srv.handleGrantConsent)
	api.DELETE("/portal/consents/:doctor", srv.authRequired("patient"), srv.handleRevokeConsent)
}

func (srv *AppServer) handleDoctors(c *gin.Context) {
	srv.authMu.Lock()
	doctors := make([]authUser, 0)
	for _, user := range srv.users {
		if user.Role == "doctor" && user.Verified {
			doctors = append(doctors, user)
		}
	}
	srv.authMu.Unlock()
	sort.Slice(doctors, func(i, j int) bool { return doctors[i].Username < doctors[j].Username })
	c.JSON(http.StatusOK, doctors)
}

func (srv *AppServer) handleConsents(c *gin.Context) {
	user := c.MustGet("auth_user").(authUser)
	items, err := srv.store.LoadConsents()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	filtered := make([]store.Consent, 0)
	for _, item := range items {
		if (user.Role == "patient" && item.Patient == user.Username) || (user.Role == "doctor" && item.Doctor == user.Username) {
			filtered = append(filtered, item)
		}
	}
	c.JSON(http.StatusOK, filtered)
}

func (srv *AppServer) handleGrantConsent(c *gin.Context) {
	patient := c.MustGet("auth_user").(authUser)
	var req struct {
		Doctor    string `json:"doctor"`
		ExpiresAt string `json:"expires_at"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid consent"})
		return
	}
	req.Doctor = strings.TrimSpace(req.Doctor)
	expiresAt, err := time.Parse(time.RFC3339, req.ExpiresAt)
	if err != nil || !expiresAt.After(time.Now()) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expiry must be a future RFC3339 time"})
		return
	}
	srv.authMu.Lock()
	doctor, ok := srv.users[req.Doctor]
	srv.authMu.Unlock()
	if !ok || doctor.Role != "doctor" || !doctor.Verified {
		c.JSON(http.StatusBadRequest, gin.H{"error": "verified doctor not found"})
		return
	}
	consent := store.Consent{Patient: patient.Username, Doctor: doctor.Username, GrantedAt: time.Now().Format(time.RFC3339), ExpiresAt: expiresAt.Format(time.RFC3339), Active: true}
	if err := srv.store.SaveConsent(consent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save consent"})
		return
	}
	c.JSON(http.StatusCreated, consent)
}

func (srv *AppServer) handleRevokeConsent(c *gin.Context) {
	patient := c.MustGet("auth_user").(authUser)
	consent, err := srv.store.GetConsent(patient.Username, c.Param("doctor"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "consent not found"})
		return
	}
	consent.Active = false
	if err := srv.store.SaveConsent(*consent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke consent"})
		return
	}
	c.JSON(http.StatusOK, consent)
}

func (srv *AppServer) consentActive(patient, doctor string) bool {
	consent, err := srv.store.GetConsent(patient, doctor)
	if err != nil || !consent.Active {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, consent.ExpiresAt)
	return err == nil && expiresAt.After(time.Now())
}
