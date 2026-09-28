package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"medtrust-raft/internal/store"

	"github.com/gin-gonic/gin"
)

func (srv *AppServer) draftRoutes(api *gin.RouterGroup) {
	api.POST("/portal/record-drafts", srv.authRequired("doctor"), srv.handleCreateRecordDraft)
	api.GET("/portal/record-drafts", srv.authRequired("patient", "doctor"), srv.handleListRecordDrafts)
	api.POST("/portal/record-drafts/:id/approve", srv.authRequired("patient"), srv.handleApproveRecordDraft)
	api.POST("/portal/record-drafts/:id/reject", srv.authRequired("patient"), srv.handleRejectRecordDraft)
	api.POST("/portal/record-drafts/:id/submit", srv.authRequired("doctor"), srv.handleSubmitRecordDraft)
}

func (srv *AppServer) handleCreateRecordDraft(c *gin.Context) {
	doctor := c.MustGet("auth_user").(authUser)
	if !doctor.Verified {
		c.JSON(http.StatusForbidden, gin.H{"error": "doctor identity is not verified"})
		return
	}
	var req struct {
		Payload map[string]interface{} `json:"payload"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid medical record"})
		return
	}
	patient, _ := req.Payload["patient_id"].(string)
	department, _ := req.Payload["department"].(string)
	diagnosis, _ := req.Payload["diagnosis"].(string)
	patient = strings.TrimSpace(patient)
	if patient == "" || strings.TrimSpace(department) == "" || strings.TrimSpace(diagnosis) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "patient, department and diagnosis are required"})
		return
	}
	srv.authMu.Lock()
	patientUser, exists := srv.users[patient]
	srv.authMu.Unlock()
	if !exists || patientUser.Role != "patient" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "patient account does not exist"})
		return
	}
	if !srv.consentActive(patient, doctor.Username) {
		c.JSON(http.StatusForbidden, gin.H{"error": "patient authorization is required"})
		return
	}
	req.Payload["patient_id"] = patient
	req.Payload["doctor_id"] = doctor.Username
	req.Payload["doctor_name"] = doctor.RealName
	req.Payload["hospital"] = doctor.Hospital
	if _, ok := req.Payload["record_id"]; !ok {
		req.Payload["record_id"] = newRecordID()
	}
	if _, ok := req.Payload["version"]; !ok {
		req.Payload["version"] = 1
	}
	req.Payload["status"] = "pending_patient_approval"
	if err := srv.validateRecordAttachments(req.Payload, patient, doctor.Username); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	srv.saveRecordDraft(c, req.Payload, patient, doctor.Username)
}

func (srv *AppServer) saveRecordDraft(c *gin.Context, record map[string]interface{}, patient, doctor string) {
	hash, err := recordContentHash(record)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to hash medical record"})
		return
	}
	ciphertext, err := srv.encryptRecord(record)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encrypt medical record"})
		return
	}
	draft := store.RecordDraft{ID: newDraftID(), Patient: patient, Doctor: doctor, Ciphertext: ciphertext, RecordHash: hash, Status: "pending_patient_approval", CreatedAt: time.Now().Format(time.RFC3339)}
	if err := srv.store.SaveRecordDraft(draft); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save record draft"})
		return
	}
	c.JSON(http.StatusCreated, publicRecordDraft(draft, record))
}

func (srv *AppServer) handleListRecordDrafts(c *gin.Context) {
	user := c.MustGet("auth_user").(authUser)
	items, err := srv.store.LoadRecordDrafts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0)
	for _, item := range items {
		if (user.Role == "patient" && item.Patient != user.Username) || (user.Role == "doctor" && item.Doctor != user.Username) {
			continue
		}
		if user.Role == "doctor" && item.Status == "pending_patient_approval" && !srv.consentActive(item.Patient, item.Doctor) {
			continue
		}
		record, err := srv.decryptRecord(item.Ciphertext)
		if err != nil {
			continue
		}
		out = append(out, publicRecordDraft(item, record))
	}
	c.JSON(http.StatusOK, out)
}

func (srv *AppServer) handleApproveRecordDraft(c *gin.Context) {
	srv.draftMu.Lock()
	defer srv.draftMu.Unlock()
	patient := c.MustGet("auth_user").(authUser)
	draft, err := srv.store.GetRecordDraft(c.Param("id"))
	if err != nil || draft.Patient != patient.Username {
		c.JSON(http.StatusNotFound, gin.H{"error": "record draft not found"})
		return
	}
	if draft.Status != "pending_patient_approval" {
		c.JSON(http.StatusConflict, gin.H{"error": "record draft is no longer awaiting approval"})
		return
	}
	if !srv.consentActive(draft.Patient, draft.Doctor) {
		c.JSON(http.StatusForbidden, gin.H{"error": "patient authorization is required"})
		return
	}
	record, err := srv.decryptRecord(draft.Ciphertext)
	if err != nil || recordContentHashMust(record) != draft.RecordHash {
		c.JSON(http.StatusConflict, gin.H{"error": "record content hash changed; approval is invalid"})
		return
	}
	draft.Status = "approved"
	draft.ApprovedAt = time.Now().Format(time.RFC3339)
	draft.ConsentID = newConsentID()
	draft.ReviewedAt = draft.ApprovedAt
	if err := srv.store.SaveRecordDraft(*draft); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save approval"})
		return
	}
	c.JSON(http.StatusOK, publicRecordDraft(*draft, record))
}

func (srv *AppServer) handleRejectRecordDraft(c *gin.Context) {
	srv.draftMu.Lock()
	defer srv.draftMu.Unlock()
	patient := c.MustGet("auth_user").(authUser)
	draft, err := srv.store.GetRecordDraft(c.Param("id"))
	if err != nil || draft.Patient != patient.Username {
		c.JSON(http.StatusNotFound, gin.H{"error": "record draft not found"})
		return
	}
	if draft.Status != "pending_patient_approval" {
		c.JSON(http.StatusConflict, gin.H{"error": "record draft is no longer awaiting approval"})
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)
	draft.Status = "rejected"
	draft.RejectedAt = time.Now().Format(time.RFC3339)
	draft.ReviewedAt = draft.RejectedAt
	draft.RejectionReason = strings.TrimSpace(req.Reason)
	if err := srv.store.SaveRecordDraft(*draft); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save rejection"})
		return
	}
	c.JSON(http.StatusOK, publicRecordDraft(*draft, nil))
}

func (srv *AppServer) handleSubmitRecordDraft(c *gin.Context) {
	srv.draftMu.Lock()
	defer srv.draftMu.Unlock()
	doctor := c.MustGet("auth_user").(authUser)
	draft, err := srv.store.GetRecordDraft(c.Param("id"))
	if err != nil || draft.Doctor != doctor.Username {
		c.JSON(http.StatusNotFound, gin.H{"error": "record draft not found"})
		return
	}
	if draft.Status != "approved" {
		c.JSON(http.StatusConflict, gin.H{"error": "patient approval is required before blockchain submission"})
		return
	}
	if !srv.consentActive(draft.Patient, doctor.Username) {
		c.JSON(http.StatusForbidden, gin.H{"error": "patient authorization is required"})
		return
	}
	record, err := srv.decryptRecord(draft.Ciphertext)
	if err != nil || recordContentHashMust(record) != draft.RecordHash {
		c.JSON(http.StatusConflict, gin.H{"error": "record content hash does not match patient approval"})
		return
	}
	record["status"] = "active"
	record["patient_consent_id"] = draft.ConsentID
	record["patient_approved_at"] = draft.ApprovedAt
	record["patient_approved_hash"] = draft.RecordHash
	srv.submitPortalRecord(c, record, draft.Patient, doctor.Username)
	if c.Writer.Status() >= http.StatusOK && c.Writer.Status() < http.StatusMultipleChoices {
		draft.Status = "submitted"
		draft.SubmittedAt = time.Now().Format(time.RFC3339)
		_ = srv.store.SaveRecordDraft(*draft)
	}
}

func recordContentHash(record map[string]interface{}) (string, error) {
	content := make(map[string]interface{}, len(record))
	for key, value := range record {
		switch key {
		case "status", "patient_consent_id", "patient_approved_at", "patient_approved_hash":
			continue
		}
		content[key] = value
	}
	plain, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(plain)
	return hex.EncodeToString(hash[:]), nil
}

func recordContentHashMust(record map[string]interface{}) string {
	hash, _ := recordContentHash(record)
	return hash
}

func publicRecordDraft(draft store.RecordDraft, record map[string]interface{}) gin.H {
	return gin.H{"id": draft.ID, "patient": draft.Patient, "doctor": draft.Doctor, "record_hash": draft.RecordHash, "status": draft.Status, "created_at": draft.CreatedAt, "reviewed_at": draft.ReviewedAt, "approved_at": draft.ApprovedAt, "rejected_at": draft.RejectedAt, "rejection_reason": draft.RejectionReason, "consent_id": draft.ConsentID, "submitted_at": draft.SubmittedAt, "submitted_block_index": draft.SubmittedBlockIndex, "certificate_id": draft.CertificateID, "record": record}
}

func newDraftID() string   { return fmt.Sprintf("DRAFT-%s", strings.ToUpper(newToken(10))) }
func newConsentID() string { return fmt.Sprintf("CHAIN-CONSENT-%s", strings.ToUpper(newToken(10))) }
func newToken(size int) string {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		panic("cannot generate secure record draft token")
	}
	return hex.EncodeToString(data)
}
