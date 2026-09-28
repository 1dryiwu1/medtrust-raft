package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"medtrust-raft/internal/store"

	"github.com/gin-gonic/gin"
)

const maxAttachmentSize = 10 << 20

func (srv *AppServer) attachmentRoutes(api *gin.RouterGroup) {
	api.POST("/portal/attachments", srv.authRequired("doctor"), srv.handleUploadAttachment)
	api.GET("/portal/attachments/:id/verify", srv.authRequired("patient", "doctor", "admin"), srv.handleVerifyAttachment)
	api.GET("/portal/attachments/:id/download", srv.authRequired("patient", "doctor"), srv.handleDownloadAttachment)
}

func (srv *AppServer) handleUploadAttachment(c *gin.Context) {
	doctor := c.MustGet("auth_user").(authUser)
	if !doctor.Verified {
		c.JSON(http.StatusForbidden, gin.H{"error": "doctor identity is not verified"})
		return
	}
	patient := strings.TrimSpace(c.PostForm("patient_id"))
	if !srv.consentActive(patient, doctor.Username) {
		c.JSON(http.StatusForbidden, gin.H{"error": "patient authorization is required"})
		return
	}
	header, err := c.FormFile("file")
	if err != nil || header.Size <= 0 || header.Size > maxAttachmentSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "attachment must be between 1 byte and 10 MB"})
		return
	}
	file, err := header.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read attachment"})
		return
	}
	defer file.Close()
	plain, err := io.ReadAll(io.LimitReader(file, maxAttachmentSize+1))
	if err != nil || len(plain) > maxAttachmentSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "attachment is too large"})
		return
	}
	ciphertext, err := srv.encryptBytes(plain)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encrypt attachment"})
		return
	}
	idBytes := make([]byte, 12)
	if _, err := rand.Read(idBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create attachment id"})
		return
	}
	id := "ATT-" + strings.ToUpper(hex.EncodeToString(idBytes))
	dir := filepath.Join("data", srv.selfID, "attachments")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare attachment storage"})
		return
	}
	path := filepath.Join(dir, id+".bin")
	if err := os.WriteFile(path, ciphertext, 0o600); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store attachment"})
		return
	}
	hash := sha256.Sum256(plain)
	meta := store.AttachmentMeta{ID: id, FileName: filepath.Base(header.Filename), ContentType: header.Header.Get("Content-Type"), Size: int64(len(plain)), SHA256: hex.EncodeToString(hash[:]), Patient: patient, Doctor: doctor.Username, StoragePath: path, CreatedAt: time.Now().Format(time.RFC3339)}
	if err := srv.store.SaveAttachment(meta); err != nil {
		_ = os.Remove(path)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save attachment metadata"})
		return
	}
	c.JSON(http.StatusCreated, publicAttachment(meta))
}

func (srv *AppServer) handleVerifyAttachment(c *gin.Context) {
	user := c.MustGet("auth_user").(authUser)
	meta, err := srv.store.GetAttachment(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "attachment not found"})
		return
	}
	if !srv.canAccessAttachment(user, meta) {
		c.JSON(http.StatusForbidden, gin.H{"error": "attachment access denied"})
		return
	}
	plain, err := srv.readAttachment(meta)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"valid": false, "attachment": publicAttachment(*meta), "message": "encrypted file cannot be verified"})
		return
	}
	hash := sha256.Sum256(plain)
	actual := hex.EncodeToString(hash[:])
	c.JSON(http.StatusOK, gin.H{"valid": actual == meta.SHA256, "expected_hash": meta.SHA256, "actual_hash": actual, "attachment": publicAttachment(*meta), "checked_at": time.Now().Format(time.RFC3339)})
}

func (srv *AppServer) handleDownloadAttachment(c *gin.Context) {
	user := c.MustGet("auth_user").(authUser)
	meta, err := srv.store.GetAttachment(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "attachment not found"})
		return
	}
	if !srv.canAccessAttachment(user, meta) {
		c.JSON(http.StatusForbidden, gin.H{"error": "attachment access denied"})
		return
	}
	plain, err := srv.readAttachment(meta)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot decrypt attachment"})
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", meta.FileName))
	c.Data(http.StatusOK, meta.ContentType, plain)
}

func (srv *AppServer) readAttachment(meta *store.AttachmentMeta) ([]byte, error) {
	ciphertext, err := os.ReadFile(meta.StoragePath)
	if err != nil {
		return nil, err
	}
	return srv.decryptBytes(ciphertext)
}

func (srv *AppServer) canAccessAttachment(user authUser, meta *store.AttachmentMeta) bool {
	if user.Role == "admin" {
		return true
	}
	if user.Role == "patient" {
		return user.Username == meta.Patient
	}
	return user.Role == "doctor" && user.Verified && srv.consentActive(meta.Patient, user.Username)
}

func publicAttachment(meta store.AttachmentMeta) gin.H {
	return gin.H{"id": meta.ID, "file_name": meta.FileName, "content_type": meta.ContentType, "size": meta.Size, "sha256": meta.SHA256, "created_at": meta.CreatedAt}
}

func (srv *AppServer) validateRecordAttachments(record map[string]interface{}, patient, doctor string) error {
	raw, exists := record["attachments"]
	if !exists {
		return nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return fmt.Errorf("invalid attachments")
	}
	validated := make([]interface{}, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid attachment metadata")
		}
		id, _ := object["id"].(string)
		meta, err := srv.store.GetAttachment(id)
		if err != nil || meta.Patient != patient || meta.Doctor != doctor {
			return fmt.Errorf("attachment %s is not owned by this submission", id)
		}
		validated = append(validated, map[string]interface{}{"id": meta.ID, "file_name": meta.FileName, "content_type": meta.ContentType, "size": meta.Size, "sha256": meta.SHA256})
	}
	record["attachments"] = validated
	return nil
}
