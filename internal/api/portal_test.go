package api

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"medtrust-raft/internal/blockchain"
	"medtrust-raft/internal/store"

	"github.com/gin-gonic/gin"
)

func TestMedicalRecordEncryptionRoundTrip(t *testing.T) {
	srv := &AppServer{portalKey: []byte("0123456789abcdef0123456789abcdef")}
	record := map[string]interface{}{"patient_id": "patient-1", "diagnosis": "private diagnosis"}
	ciphertext, err := srv.encryptRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, "private diagnosis") {
		t.Fatal("ciphertext contains plaintext diagnosis")
	}
	plain, err := srv.decryptRecord(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if plain["diagnosis"] != record["diagnosis"] {
		t.Fatalf("unexpected decrypted record: %#v", plain)
	}
}

func TestIdentityReferencesAreStableAndSeparated(t *testing.T) {
	srv := &AppServer{portalKey: []byte("0123456789abcdef0123456789abcdef")}
	if srv.identityRef("patient-a") != srv.identityRef("patient-a") {
		t.Fatal("identity reference is not stable")
	}
	if srv.identityRef("patient-a") == srv.identityRef("patient-b") {
		t.Fatal("different identities share a reference")
	}
}

func TestConsentExpirationAndRevocationBlockDoctorAttachmentAccess(t *testing.T) {
	srv := newPortalTestServer(t)
	meta := &store.AttachmentMeta{ID: "ATT-TEST", Patient: "patient-a", Doctor: "doctor-a"}
	doctor := authUser{Username: "doctor-a", Role: "doctor", Verified: true}

	active := store.Consent{Patient: "patient-a", Doctor: "doctor-a", Active: true, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}
	if err := srv.store.SaveConsent(active); err != nil {
		t.Fatal(err)
	}
	if !srv.consentActive("patient-a", "doctor-a") || !srv.canAccessAttachment(doctor, meta) {
		t.Fatal("active consent did not grant attachment access")
	}

	active.ExpiresAt = time.Now().Add(-time.Minute).Format(time.RFC3339)
	if err := srv.store.SaveConsent(active); err != nil {
		t.Fatal(err)
	}
	if srv.consentActive("patient-a", "doctor-a") || srv.canAccessAttachment(doctor, meta) {
		t.Fatal("expired consent still grants attachment access")
	}

	active.ExpiresAt = time.Now().Add(time.Hour).Format(time.RFC3339)
	active.Active = false
	if err := srv.store.SaveConsent(active); err != nil {
		t.Fatal(err)
	}
	if srv.consentActive("patient-a", "doctor-a") || srv.canAccessAttachment(doctor, meta) {
		t.Fatal("revoked consent still grants attachment access")
	}
}

func TestMarkSupersededVersions(t *testing.T) {
	blocks := []*blockchain.Block{
		{Index: 10, Payload: `{"record":{"record_id":"MR-1","version":1,"status":"active"}}`},
		{Index: 11, Payload: `{"record":{"record_id":"MR-1","version":2,"status":"active"}}`},
		{Index: 12, Payload: `{"record":{"record_id":"MR-2","version":1,"status":"active"}}`},
	}
	markSupersededVersions(blocks)
	if !strings.Contains(blocks[0].Payload, `"status":"superseded"`) {
		t.Fatalf("old version was not superseded: %s", blocks[0].Payload)
	}
	if strings.Contains(blocks[1].Payload, `"status":"superseded"`) || strings.Contains(blocks[2].Payload, `"status":"superseded"`) {
		t.Fatal("latest or unrelated record version was superseded")
	}
}

func TestAttachmentEncryptedFileRoundTrip(t *testing.T) {
	srv := newPortalTestServer(t)
	plain := []byte("private imaging report: diagnosis must remain confidential")
	sealed, err := srv.encryptBytes(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plain) {
		t.Fatal("encrypted attachment contains plaintext")
	}
	path := filepath.Join(t.TempDir(), "attachment.bin")
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := srv.readAttachment(&store.AttachmentMeta{StoragePath: path})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, plain) {
		t.Fatal("decrypted attachment differs from original bytes")
	}
}

func TestRecordContentHashLocksPatientApprovedFields(t *testing.T) {
	record := map[string]interface{}{"patient_id": "patient-a", "diagnosis": "initial", "attachments": []interface{}{map[string]interface{}{"sha256": "abc"}}, "status": "pending_patient_approval"}
	original, err := recordContentHash(record)
	if err != nil {
		t.Fatal(err)
	}
	record["status"] = "active"
	record["patient_consent_id"] = "CHAIN-CONSENT-1"
	if changed := recordContentHashMust(record); changed != original {
		t.Fatal("workflow metadata changed the approved medical-content hash")
	}
	record["diagnosis"] = "modified after approval"
	if changed := recordContentHashMust(record); changed == original {
		t.Fatal("medical content changed without changing the approved hash")
	}
}

func TestPatientApprovalIsSpecificAndOneTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := newPortalTestServer(t)
	consent := store.Consent{Patient: "patient-a", Doctor: "doctor-a", Active: true, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}
	if err := srv.store.SaveConsent(consent); err != nil {
		t.Fatal(err)
	}
	record := map[string]interface{}{"patient_id": "patient-a", "doctor_id": "doctor-a", "diagnosis": "approved diagnosis", "status": "pending_patient_approval"}
	ciphertext, err := srv.encryptRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	draft := store.RecordDraft{ID: "DRAFT-1", Patient: "patient-a", Doctor: "doctor-a", Ciphertext: ciphertext, RecordHash: recordContentHashMust(record), Status: "pending_patient_approval", CreatedAt: time.Now().Format(time.RFC3339)}
	if err := srv.store.SaveRecordDraft(draft); err != nil {
		t.Fatal(err)
	}

	approve := func() int {
		writer := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(writer)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/portal/record-drafts/DRAFT-1/approve", nil)
		ctx.Params = gin.Params{{Key: "id", Value: "DRAFT-1"}}
		ctx.Set("auth_user", authUser{Username: "patient-a", Role: "patient"})
		srv.handleApproveRecordDraft(ctx)
		return writer.Code
	}
	if status := approve(); status != http.StatusOK {
		t.Fatalf("first approval returned %d", status)
	}
	approved, err := srv.store.GetRecordDraft("DRAFT-1")
	if err != nil || approved.Status != "approved" || approved.ConsentID == "" {
		t.Fatalf("approval credential was not persisted: %#v, %v", approved, err)
	}
	if status := approve(); status != http.StatusConflict {
		t.Fatalf("duplicate approval returned %d, want 409", status)
	}
}

func TestLegacyPortalUploadCreatesDraftInsteadOfBlockchainWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := newPortalTestServer(t)
	srv.users = map[string]authUser{"patient-a": {Username: "patient-a", Role: "patient"}}
	if err := srv.store.SaveConsent(store.Consent{Patient: "patient-a", Doctor: "doctor-a", Active: true, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]interface{}{"payload": map[string]interface{}{"patient_id": "patient-a", "department": "cardiology", "diagnosis": "draft only"}})
	writer := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(writer)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/portal/record/upload", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("auth_user", authUser{Username: "doctor-a", Role: "doctor", RealName: "Doctor A", Hospital: "Hospital A", Verified: true})
	srv.handlePortalUpload(ctx)
	if writer.Code != http.StatusCreated {
		t.Fatalf("legacy upload returned %d: %s", writer.Code, writer.Body.String())
	}
	drafts, err := srv.store.LoadRecordDrafts()
	if err != nil || len(drafts) != 1 || drafts[0].Status != "pending_patient_approval" {
		t.Fatalf("upload bypassed draft workflow: %#v, %v", drafts, err)
	}
}

func TestRawBlockchainWriteRequiresPortalApprovalSignature(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := &AppServer{apiKey: "shared-cluster-secret"}
	body := []byte(`{"payload":{"ciphertext":"approved-record"}}`)
	run := func(signature string) (int, bool) {
		writer := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(writer)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/record/upload", bytes.NewReader(body))
		ctx.Request.Header.Set("X-Portal-Approval-Signature", signature)
		called := false
		srv.approvedPortalWriteRequired()(ctx)
		if !ctx.IsAborted() {
			called = true
			ctx.Status(http.StatusNoContent)
			ctx.Writer.WriteHeaderNow()
		}
		return writer.Code, called
	}
	if status, called := run(""); status != http.StatusForbidden || called {
		t.Fatalf("unsigned write was allowed: status=%d called=%v", status, called)
	}
	if status, called := run("00"); status != http.StatusForbidden || called {
		t.Fatalf("invalid signature was allowed: status=%d called=%v", status, called)
	}
	valid := hex.EncodeToString(srv.portalWriteSignature(body))
	if status, called := run(valid); status != http.StatusNoContent || !called {
		t.Fatalf("valid portal signature was rejected: status=%d called=%v", status, called)
	}
}

func newPortalTestServer(t *testing.T) *AppServer {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &AppServer{store: db, portalKey: []byte("0123456789abcdef0123456789abcdef")}
}
