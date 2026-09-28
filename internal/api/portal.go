package api

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"medtrust-raft/internal/blockchain"

	"github.com/gin-gonic/gin"
)

func (srv *AppServer) portalRoutes(api *gin.RouterGroup) {
	api.GET("/portal/records", srv.authRequired("patient", "doctor", "admin"), srv.handlePortalRecords)
	api.POST("/portal/record/upload", srv.authRequired("doctor"), srv.handlePortalUpload)
	api.POST("/portal/records/:index/correct", srv.authRequired("doctor"), srv.handleCorrectRecord)
	srv.consentRoutes(api)
	srv.attachmentRoutes(api)
	srv.draftRoutes(api)
}

func (srv *AppServer) handlePortalRecords(c *gin.Context) {
	user := c.MustGet("auth_user").(authUser)
	blocks, err := srv.store.AllBlocks(0, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if user.Role == "admin" {
		c.JSON(http.StatusOK, blocks)
		return
	}
	filtered := make([]*blockchain.Block, 0)
	wantedRef := srv.identityRef(user.Username)
	grantedPatientRefs := make(map[string]bool)
	if user.Role == "doctor" {
		consents, _ := srv.store.LoadConsents()
		for _, consent := range consents {
			if consent.Doctor == user.Username && srv.consentActive(consent.Patient, user.Username) {
				grantedPatientRefs[srv.identityRef(consent.Patient)] = true
			}
		}
	}
	for _, block := range blocks {
		var envelope medicalEnvelope
		if json.Unmarshal([]byte(block.Payload), &envelope) != nil {
			continue
		}
		if user.Role == "patient" && envelope.Record.PatientRef != wantedRef {
			continue
		}
		if user.Role == "doctor" && !grantedPatientRefs[envelope.Record.PatientRef] {
			continue
		}
		plain, err := srv.decryptRecord(envelope.Record.Ciphertext)
		if err != nil {
			continue
		}
		viewPayload, _ := json.Marshal(map[string]interface{}{"certificate_id": envelope.CertificateID, "data_hash": envelope.DataHash, "hash_algorithm": envelope.HashAlgorithm, "submitted_at": envelope.SubmittedAt, "record": plain})
		copyBlock := *block
		copyBlock.Payload = string(viewPayload)
		filtered = append(filtered, &copyBlock)
	}
	markSupersededVersions(filtered)
	c.JSON(http.StatusOK, filtered)
}

func (srv *AppServer) handlePortalUpload(c *gin.Context) {
	// Kept for compatibility with older clients; portal records must always
	// enter the patient-approval workflow before blockchain submission.
	srv.handleCreateRecordDraft(c)
}

func (srv *AppServer) submitPortalRecord(c *gin.Context, record map[string]interface{}, patient, doctor string) {
	ciphertext, err := srv.encryptRecord(record)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encrypt medical record"})
		return
	}
	wrapped := map[string]interface{}{
		"format":                "medtrust-aes-gcm-v1",
		"patient_ref":           srv.identityRef(patient),
		"doctor_ref":            srv.identityRef(doctor),
		"ciphertext":            ciphertext,
		"record_id":             record["record_id"],
		"version":               record["version"],
		"previous_index":        record["previous_index"],
		"attachment_hashes":     attachmentHashes(record["attachments"]),
		"patient_consent_id":    record["patient_consent_id"],
		"patient_approved_at":   record["patient_approved_at"],
		"patient_approved_hash": record["patient_approved_hash"],
	}
	body, _ := json.Marshal(map[string]interface{}{"payload": wrapped})
	if srv.node.IsLeader() {
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Request.Header.Set("X-Portal-Approval-Signature", fmt.Sprintf("%x", srv.portalWriteSignature(body)))
		srv.handleUpload(c)
		return
	}
	leader := srv.leaderAddr()
	if leader == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster leader is being elected"})
		return
	}
	httpReq, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, leader+"/api/record/upload", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	if srv.apiKey != "" {
		httpReq.Header.Set("X-API-Key", srv.apiKey)
	}
	httpReq.Header.Set("X-Portal-Approval-Signature", fmt.Sprintf("%x", srv.portalWriteSignature(body)))
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "leader unavailable"})
		return
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, "application/json", responseBody)
}

func attachmentHashes(value interface{}) []string {
	items, _ := value.([]interface{})
	hashes := make([]string, 0, len(items))
	for _, item := range items {
		object, _ := item.(map[string]interface{})
		hash, _ := object["sha256"].(string)
		if hash != "" {
			hashes = append(hashes, hash)
		}
	}
	return hashes
}

func (srv *AppServer) handleCorrectRecord(c *gin.Context) {
	doctor := c.MustGet("auth_user").(authUser)
	if !doctor.Verified {
		c.JSON(http.StatusForbidden, gin.H{"error": "doctor identity is not verified"})
		return
	}
	index, err := strconv.ParseUint(c.Param("index"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid block index"})
		return
	}
	block, err := srv.store.GetBlock(index)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "record not found"})
		return
	}
	var envelope medicalEnvelope
	if json.Unmarshal([]byte(block.Payload), &envelope) != nil || envelope.Record.DoctorRef != srv.identityRef(doctor.Username) {
		c.JSON(http.StatusForbidden, gin.H{"error": "only the submitting doctor can correct this record"})
		return
	}
	original, err := srv.decryptRecord(envelope.Record.Ciphertext)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot decrypt original record"})
		return
	}
	patient, _ := original["patient_id"].(string)
	if !srv.isLatestRecordVersion(original) {
		c.JSON(http.StatusConflict, gin.H{"error": "only the latest record version can be corrected"})
		return
	}
	if !srv.consentActive(patient, doctor.Username) {
		c.JSON(http.StatusForbidden, gin.H{"error": "patient authorization is required"})
		return
	}
	var req struct {
		Diagnosis  string `json:"diagnosis"`
		Notes      string `json:"notes"`
		Department string `json:"department"`
		Type       string `json:"type"`
		Reason     string `json:"reason"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Reason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "correction reason is required"})
		return
	}
	if strings.TrimSpace(req.Diagnosis) != "" {
		original["diagnosis"] = strings.TrimSpace(req.Diagnosis)
	}
	if strings.TrimSpace(req.Notes) != "" {
		original["notes"] = strings.TrimSpace(req.Notes)
	}
	if strings.TrimSpace(req.Department) != "" {
		original["department"] = strings.TrimSpace(req.Department)
	}
	if strings.TrimSpace(req.Type) != "" {
		original["type"] = strings.TrimSpace(req.Type)
	}
	version := int(numberValue(original["version"])) + 1
	original["version"] = version
	original["previous_index"] = index
	original["correction_reason"] = strings.TrimSpace(req.Reason)
	original["corrected_at"] = time.Now().Format(time.RFC3339)
	original["status"] = "active"
	original["status"] = "pending_patient_approval"
	delete(original, "patient_consent_id")
	delete(original, "patient_approved_at")
	delete(original, "patient_approved_hash")
	srv.saveRecordDraft(c, original, patient, doctor.Username)
}

func (srv *AppServer) isLatestRecordVersion(record map[string]interface{}) bool {
	recordID, _ := record["record_id"].(string)
	if recordID == "" {
		return true
	}
	currentVersion := numberValue(record["version"])
	blocks, err := srv.store.AllBlocks(0, 0)
	if err != nil {
		return false
	}
	for _, block := range blocks {
		var envelope medicalEnvelope
		if json.Unmarshal([]byte(block.Payload), &envelope) != nil {
			continue
		}
		candidate, err := srv.decryptRecord(envelope.Record.Ciphertext)
		if err != nil {
			continue
		}
		candidateID, _ := candidate["record_id"].(string)
		if candidateID == recordID && numberValue(candidate["version"]) > currentVersion {
			return false
		}
	}
	return true
}

type medicalEnvelope struct {
	CertificateID string `json:"certificate_id"`
	DataHash      string `json:"data_hash"`
	HashAlgorithm string `json:"hash_algorithm"`
	SubmittedAt   string `json:"submitted_at"`
	Record        struct {
		PatientRef string `json:"patient_ref"`
		DoctorRef  string `json:"doctor_ref"`
		Ciphertext string `json:"ciphertext"`
	} `json:"record"`
}

func newRecordID() string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return fmt.Sprintf("MR-%s-%s", time.Now().Format("20060102"), strings.ToUpper(fmt.Sprintf("%x", bytes)))
}

func numberValue(value interface{}) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case int:
		return float64(number)
	case json.Number:
		value, _ := number.Float64()
		return value
	}
	return 0
}

func markSupersededVersions(blocks []*blockchain.Block) {
	latest := make(map[string]float64)
	for _, block := range blocks {
		var envelope struct {
			Record map[string]interface{} `json:"record"`
		}
		if json.Unmarshal([]byte(block.Payload), &envelope) != nil {
			continue
		}
		id, _ := envelope.Record["record_id"].(string)
		version := numberValue(envelope.Record["version"])
		if version > latest[id] {
			latest[id] = version
		}
	}
	for _, block := range blocks {
		var envelope map[string]interface{}
		if json.Unmarshal([]byte(block.Payload), &envelope) != nil {
			continue
		}
		record, _ := envelope["record"].(map[string]interface{})
		id, _ := record["record_id"].(string)
		version := numberValue(record["version"])
		if id != "" && version < latest[id] {
			record["status"] = "superseded"
		}
		payload, _ := json.Marshal(envelope)
		block.Payload = string(payload)
	}
}

func (srv *AppServer) identityRef(identity string) string {
	mac := hmac.New(sha256.New, srv.portalKey)
	_, _ = mac.Write([]byte(identity))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (srv *AppServer) encryptRecord(record map[string]interface{}) (string, error) {
	plain, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	sealed, err := srv.encryptBytes(plain)
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (srv *AppServer) decryptRecord(encoded string) (map[string]interface{}, error) {
	sealed, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	plain, err := srv.decryptBytes(sealed)
	if err != nil {
		return nil, err
	}
	var record map[string]interface{}
	err = json.Unmarshal(plain, &record)
	return record, err
}

func (srv *AppServer) encryptBytes(plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(srv.portalKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, nil), nil
}

func (srv *AppServer) decryptBytes(sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(srv.portalKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize() {
		return nil, io.ErrUnexpectedEOF
	}
	return aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], nil)
}
