package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/kyenel64/invosit-api/internal/httpx"
)

// stubStorage records calls and returns canned values so handler tests can
// assert behaviour without spinning up R2/S3.
type stubStorage struct {
	putURL, getURL string
	putErr         error
	getErr         error
	deleteErr      error
	deletedKey     string
	deleteCalls    int
}

func (s *stubStorage) SignedPutURL(_ context.Context, _ string, _ time.Duration) (string, error) {
	return s.putURL, s.putErr
}
func (s *stubStorage) SignedGetURL(_ context.Context, _ string, _ time.Duration) (string, error) {
	return s.getURL, s.getErr
}
func (s *stubStorage) Delete(_ context.Context, key string) error {
	s.deleteCalls++
	s.deletedKey = key
	return s.deleteErr
}

const validHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func pushCtx() context.Context {
	ctx := httpx.WithUserID(context.Background(), "usr_abc")
	ctx = httpx.WithWorkspaceID(ctx, "ws_abc")
	ctx = httpx.WithEnvironmentID(ctx, "env_abc")
	ctx = httpx.WithWorkspaceRole(ctx, "member")
	return ctx
}

func TestPushFile_Success(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO files`).
		WithArgs(sqlmock.AnyArg(), "ws_abc", "env_abc", "config/.env", validHash, int64(123), "usr_abc", sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("file_xyz"))
	mock.ExpectExec(`UPDATE file_versions SET is_current = FALSE`).
		WithArgs("file_xyz").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO file_versions`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	stub := &stubStorage{putURL: "https://signed/put"}
	h := &Handler{db: db, blobs: stub}

	body := `{"path":"config/.env","content_hash":"` + validHash + `","size":123}`
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)).WithContext(pushCtx())
	rec := httptest.NewRecorder()
	h.PushFile(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["upload_url"] != "https://signed/put" {
		t.Errorf("upload_url = %v", got["upload_url"])
	}
	if got["id"] != "file_xyz" {
		t.Errorf("id = %v", got["id"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestPushFile_ViewerForbidden(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()

	h := &Handler{db: db, blobs: &stubStorage{}}
	ctx := pushCtx()
	ctx = httpx.WithWorkspaceRole(ctx, "viewer")

	body := `{"path":"a","content_hash":"` + validHash + `","size":1}`
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.PushFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestPushFile_NoUserID(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()

	h := &Handler{db: db, blobs: &stubStorage{}}
	body := `{"path":"a","content_hash":"` + validHash + `","size":1}`
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.PushFile(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestPushFile_RejectsBadPaths(t *testing.T) {
	bad := []string{
		"/abs/path",
		"\\windows\\abs",
		"../escape",
		"a/../b",
		"a\\..\\b",
		"with\x00null",
		"",
	}
	for _, p := range bad {
		t.Run(p, func(t *testing.T) {
			db, _, _ := sqlmock.New()
			defer db.Close()
			h := &Handler{db: db, blobs: &stubStorage{}}

			body, _ := json.Marshal(map[string]any{"path": p, "content_hash": validHash, "size": 1})
			req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(string(body))).WithContext(pushCtx())
			rec := httptest.NewRecorder()
			h.PushFile(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body = %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPushFile_RejectsBadHash(t *testing.T) {
	bad := []string{
		"tooshort",
		strings.Repeat("z", 64),            // not hex
		strings.Repeat("a", 63),            // wrong length
		strings.Repeat("A", 64),            // uppercase — blob key would diverge
		"ABCDEF" + strings.Repeat("a", 58), // mixed case at start
		strings.Repeat("a", 30) + "F" + strings.Repeat("a", 33), // single uppercase in middle
	}
	for _, hash := range bad {
		t.Run(hash, func(t *testing.T) {
			db, _, _ := sqlmock.New()
			defer db.Close()
			h := &Handler{db: db, blobs: &stubStorage{}}

			body, _ := json.Marshal(map[string]any{"path": "a.txt", "content_hash": hash, "size": 1})
			req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(string(body))).WithContext(pushCtx())
			rec := httptest.NewRecorder()
			h.PushFile(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

// If presigning fails, the transaction must roll back so we don't end up
// with files.content_hash pointing at a blob the client was never able to
// upload. Verifies the ordering: all DB work happens inside the tx, presign
// happens before Commit.
func TestPushFile_RollsBackWhenPresignFails(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO files`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("file_xyz"))
	mock.ExpectExec(`UPDATE file_versions SET is_current = FALSE`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`INSERT INTO file_versions`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback() // No ExpectCommit — presign failure should keep the tx open for rollback.

	h := &Handler{db: db, blobs: &stubStorage{putErr: errors.New("storage down")}}
	body := `{"path":"a","content_hash":"` + validHash + `","size":1}`
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)).WithContext(pushCtx())
	rec := httptest.NewRecorder()
	h.PushFile(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestPushFile_RollsBackOnInsertFailure(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO files`).
		WillReturnError(errors.New("boom"))
	mock.ExpectRollback()

	h := &Handler{db: db, blobs: &stubStorage{}}
	body := `{"path":"a","content_hash":"` + validHash + `","size":1}`
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)).WithContext(pushCtx())
	rec := httptest.NewRecorder()
	h.PushFile(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestListFiles_Success(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	pushedAt := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT id, path, content_hash, size, pushed_by, pushed_at\s+FROM files`).
		WithArgs("env_abc").
		WillReturnRows(sqlmock.NewRows([]string{"id", "path", "content_hash", "size", "pushed_by", "pushed_at"}).
			AddRow("file_a", "a.env", validHash, int64(10), "usr_abc", pushedAt).
			AddRow("file_b", "b.env", validHash, int64(20), "usr_xyz", pushedAt))

	h := &Handler{db: db}
	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(pushCtx())
	rec := httptest.NewRecorder()
	h.ListFiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Files) != 2 {
		t.Fatalf("len = %d", len(got.Files))
	}
}

func TestListFiles_Empty(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery(`SELECT id, path, content_hash, size, pushed_by, pushed_at\s+FROM files`).
		WithArgs("env_abc").
		WillReturnRows(sqlmock.NewRows([]string{"id", "path", "content_hash", "size", "pushed_by", "pushed_at"}))

	h := &Handler{db: db}
	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(pushCtx())
	rec := httptest.NewRecorder()
	h.ListFiles(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Files []map[string]any `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Files == nil || len(got.Files) != 0 {
		t.Errorf("files = %+v, want empty slice (not null)", got.Files)
	}
}

func TestGetFile_Success(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	pushedAt := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT path, content_hash, size, pushed_by, pushed_at\s+FROM files`).
		WithArgs("file_xyz", "env_abc").
		WillReturnRows(sqlmock.NewRows([]string{"path", "content_hash", "size", "pushed_by", "pushed_at"}).
			AddRow("a.env", validHash, int64(123), "usr_abc", pushedAt))

	stub := &stubStorage{getURL: "https://signed/get"}
	h := &Handler{db: db, blobs: stub}

	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(pushCtx())
	req.SetPathValue("fileId", "file_xyz")
	rec := httptest.NewRecorder()
	h.GetFile(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["download_url"] != "https://signed/get" {
		t.Errorf("download_url = %v", got["download_url"])
	}
	if got["path"] != "a.env" {
		t.Errorf("path = %v", got["path"])
	}
}

func TestGetFile_NotFoundReturns403(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery(`SELECT path, content_hash`).
		WithArgs("file_missing", "env_abc").
		WillReturnError(sql.ErrNoRows)

	h := &Handler{db: db, blobs: &stubStorage{}}
	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(pushCtx())
	req.SetPathValue("fileId", "file_missing")
	rec := httptest.NewRecorder()
	h.GetFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestDeleteFile_Success(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery(`SELECT content_hash FROM files`).
		WithArgs("file_xyz", "env_abc").
		WillReturnRows(sqlmock.NewRows([]string{"content_hash"}).AddRow(validHash))
	mock.ExpectExec(`DELETE FROM files WHERE id = \$1 AND environment_id = \$2`).
		WithArgs("file_xyz", "env_abc").
		WillReturnResult(sqlmock.NewResult(0, 1))

	stub := &stubStorage{}
	h := &Handler{db: db, blobs: stub}
	req := httptest.NewRequest(http.MethodDelete, "/x", nil).WithContext(pushCtx())
	req.SetPathValue("fileId", "file_xyz")
	rec := httptest.NewRecorder()
	h.DeleteFile(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 (body = %s)", rec.Code, rec.Body.String())
	}
	if stub.deleteCalls != 1 {
		t.Errorf("storage.Delete calls = %d, want 1", stub.deleteCalls)
	}
	if stub.deletedKey != "ws_abc/"+validHash {
		t.Errorf("deleted key = %q", stub.deletedKey)
	}
}

func TestDeleteFile_ViewerForbidden(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer db.Close()

	h := &Handler{db: db, blobs: &stubStorage{}}
	ctx := pushCtx()
	ctx = httpx.WithWorkspaceRole(ctx, "viewer")

	req := httptest.NewRequest(http.MethodDelete, "/x", nil).WithContext(ctx)
	req.SetPathValue("fileId", "file_xyz")
	rec := httptest.NewRecorder()
	h.DeleteFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestDeleteFile_MissingReturns403(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery(`SELECT content_hash FROM files`).
		WithArgs("file_missing", "env_abc").
		WillReturnError(sql.ErrNoRows)

	h := &Handler{db: db, blobs: &stubStorage{}}
	req := httptest.NewRequest(http.MethodDelete, "/x", nil).WithContext(pushCtx())
	req.SetPathValue("fileId", "file_missing")
	rec := httptest.NewRecorder()
	h.DeleteFile(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// Blob storage failures must not propagate as a 500 — the DB delete already
// succeeded and rolling that back is impossible. We log and return 204.
func TestDeleteFile_BlobDeleteFailureStillReturns204(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()

	mock.ExpectQuery(`SELECT content_hash FROM files`).
		WithArgs("file_xyz", "env_abc").
		WillReturnRows(sqlmock.NewRows([]string{"content_hash"}).AddRow(validHash))
	mock.ExpectExec(`DELETE FROM files`).
		WithArgs("file_xyz", "env_abc").
		WillReturnResult(sqlmock.NewResult(0, 1))

	h := &Handler{db: db, blobs: &stubStorage{deleteErr: errors.New("boom")}}
	req := httptest.NewRequest(http.MethodDelete, "/x", nil).WithContext(pushCtx())
	req.SetPathValue("fileId", "file_xyz")
	rec := httptest.NewRecorder()
	h.DeleteFile(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}
