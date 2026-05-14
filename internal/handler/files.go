package handler

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/kyenel64/invosit-api/internal/httpx"
	"github.com/kyenel64/invosit-api/internal/ids"
	"github.com/kyenel64/invosit-api/internal/storage"
)

type pushFileRequest struct {
	Path        string `json:"path"         validate:"required,max=1024"`
	ContentHash string `json:"content_hash" validate:"required,len=64"`
	Size        int64  `json:"size"         validate:"required,gt=0"`
	Message     string `json:"message"      validate:"max=512"`
}

// PushFile registers a new version of a file in the environment and returns
// a short-lived signed PUT URL the client uses to upload the (encrypted)
// blob directly to storage. The DB row is created before the URL is issued
// so a failed upload leaves an orphan version, not a missing one — that
// trade keeps the wire protocol simple and matches the issue spec.
//
// In the unencrypted MVP, content_hash is the sha256 of the plaintext.
// Once M4 lands, the CLI will hash the ciphertext and add wrapped_deks.
func (h *Handler) PushFile(w http.ResponseWriter, r *http.Request) {
	uid := httpx.UserID(r.Context())
	if uid == "" {
		httpx.RespondError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	workspaceID := httpx.WorkspaceID(r.Context())
	envID := httpx.EnvironmentID(r.Context())
	role := httpx.WorkspaceRole(r.Context())

	if role == "viewer" {
		httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "write permission required")
		return
	}

	var req pushFileRequest
	if err := httpx.Bind(r, &req); err != nil {
		httpx.RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid push request")
		return
	}
	path := strings.TrimSpace(req.Path)
	if err := validateFilePath(path); err != nil {
		httpx.RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid path")
		return
	}
	if err := validateSha256Hex(req.ContentHash); err != nil {
		httpx.RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid content hash")
		return
	}

	blobKey := workspaceID + "/" + req.ContentHash
	pushedAt := time.Now().UTC()
	fileID := ids.File()
	versionID := ids.Version()

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	// Upsert the files row. On conflict (env_id, path) the existing row is
	// updated to point at the new content; its id is returned either way.
	err = tx.QueryRowContext(r.Context(),
		`INSERT INTO files (id, workspace_id, environment_id, path, content_hash, size, pushed_by, pushed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (environment_id, path) DO UPDATE
		   SET content_hash = EXCLUDED.content_hash,
		       size         = EXCLUDED.size,
		       pushed_by    = EXCLUDED.pushed_by,
		       pushed_at    = EXCLUDED.pushed_at
		 RETURNING id`,
		fileID, workspaceID, envID, path, req.ContentHash, req.Size, uid, pushedAt,
	).Scan(&fileID)
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	// Demote the prior current version before inserting the new one — the
	// partial unique index on file_versions (file_id) WHERE is_current
	// would otherwise reject the insert.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE file_versions SET is_current = FALSE WHERE file_id = $1 AND is_current = TRUE`,
		fileID,
	); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	if _, err := tx.ExecContext(r.Context(),
		`INSERT INTO file_versions (id, file_id, blob_key, content_hash, size, pushed_by, pushed_at, message, is_current)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, TRUE)`,
		versionID, fileID, blobKey, req.ContentHash, req.Size, uid, pushedAt, nullableString(req.Message),
	); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	// Presign before committing. If signing fails (storage outage, mis-config),
	// the deferred Rollback discards the version so a subsequent pull doesn't
	// point at a blob the client was never given a chance to upload.
	uploadURL, err := h.blobs.SignedPutURL(r.Context(), blobKey, storage.MaxSignedURLExpiry)
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	if err := tx.Commit(); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"id":                fileID,
		"environment_id":    envID,
		"path":              path,
		"content_hash":      req.ContentHash,
		"size":              req.Size,
		"pushed_by":         uid,
		"pushed_at":         pushedAt,
		"version_id":        versionID,
		"upload_url":        uploadURL,
		"upload_expires_at": pushedAt.Add(storage.MaxSignedURLExpiry),
	})
}

// ListFiles returns the current state of every file in the environment.
// The middleware chain has already confirmed env membership.
func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	envID := httpx.EnvironmentID(r.Context())

	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, path, content_hash, size, pushed_by, pushed_at
		   FROM files
		  WHERE environment_id = $1
		  ORDER BY path ASC`,
		envID,
	)
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	defer func() { _ = rows.Close() }()

	files := []map[string]any{}
	for rows.Next() {
		var (
			id, path, hash string
			size           int64
			pushedBy       sql.NullString
			pushedAt       time.Time
		)
		if err := rows.Scan(&id, &path, &hash, &size, &pushedBy, &pushedAt); err != nil {
			httpx.InternalError(w, r, err)
			return
		}
		files = append(files, map[string]any{
			"id":           id,
			"path":         path,
			"content_hash": hash,
			"size":         size,
			"pushed_by":    pushedBy.String,
			"pushed_at":    pushedAt,
		})
	}
	if err := rows.Err(); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"files": files})
}

// GetFile returns metadata plus a short-lived signed GET URL for the
// current version's blob. Membership is already verified by middleware
func (h *Handler) GetFile(w http.ResponseWriter, r *http.Request) {
	workspaceID := httpx.WorkspaceID(r.Context())
	envID := httpx.EnvironmentID(r.Context())
	fileID := r.PathValue("fileId")
	if fileID == "" {
		httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
		return
	}

	var (
		path, hash string
		size       int64
		pushedBy   sql.NullString
		pushedAt   time.Time
	)
	err := h.db.QueryRowContext(r.Context(),
		`SELECT path, content_hash, size, pushed_by, pushed_at
		   FROM files
		  WHERE id = $1 AND environment_id = $2`,
		fileID, envID,
	).Scan(&path, &hash, &size, &pushedBy, &pushedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
			return
		}
		httpx.InternalError(w, r, err)
		return
	}

	blobKey := workspaceID + "/" + hash
	downloadURL, err := h.blobs.SignedGetURL(r.Context(), blobKey, storage.MaxSignedURLExpiry)
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id":                  fileID,
		"environment_id":      envID,
		"path":                path,
		"content_hash":        hash,
		"size":                size,
		"pushed_by":           pushedBy.String,
		"pushed_at":           pushedAt,
		"download_url":        downloadURL,
		"download_expires_at": time.Now().UTC().Add(storage.MaxSignedURLExpiry),
	})
}

// DeleteFile removes the files row (cascades to file_versions and
// wrapped_deks) and best-effort deletes the orphaned blob.
//
// In the unencrypted MVP two files in the same workspace can share a
// blob key (same content_hash → same key). Deleting one would 404 the
// other. Once encryption lands in M4, per-file DEKs make every ciphertext
// unique by construction, so the collision goes away. Acceptable for
// the plumbing-validation iteration.
func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	workspaceID := httpx.WorkspaceID(r.Context())
	envID := httpx.EnvironmentID(r.Context())
	role := httpx.WorkspaceRole(r.Context())
	fileID := r.PathValue("fileId")
	if fileID == "" {
		httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
		return
	}
	if role == "viewer" {
		httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "write permission required")
		return
	}

	var contentHash string
	err := h.db.QueryRowContext(r.Context(),
		`SELECT content_hash FROM files WHERE id = $1 AND environment_id = $2`,
		fileID, envID,
	).Scan(&contentHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
			return
		}
		httpx.InternalError(w, r, err)
		return
	}

	res, err := h.db.ExecContext(r.Context(),
		`DELETE FROM files WHERE id = $1 AND environment_id = $2`,
		fileID, envID,
	)
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	affected, err := res.RowsAffected()
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	if affected == 0 {
		httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
		return
	}

	blobKey := workspaceID + "/" + contentHash
	if err := h.blobs.Delete(r.Context(), blobKey); err != nil {
		// Orphan blob is recoverable via a sweep; failing the request would
		// suggest the DB row is still there, which would be misleading.
		log.Printf("req=%s blob_delete_failed key=%q err=%v",
			httpx.RequestID(r.Context()), blobKey, err)
	}

	w.WriteHeader(http.StatusNoContent)
}

type rollbackRequest struct {
	VersionID string `json:"version_id" validate:"required,startswith=ver_,max=64"`
}

// Returns the version history of a file, newest first.
func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	envID := httpx.EnvironmentID(r.Context())
	fileID := r.PathValue("fileId")
	if fileID == "" {
		httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
		return
	}

	var exists string
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id FROM files WHERE id = $1 AND environment_id = $2`,
		fileID, envID,
	).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
			return
		}
		httpx.InternalError(w, r, err)
		return
	}

	rows, err := h.db.QueryContext(r.Context(),
		`SELECT id, file_id, content_hash, size, pushed_by, pushed_at, message, is_current
		   FROM file_versions
		  WHERE file_id = $1
		  ORDER BY pushed_at DESC, id DESC`,
		fileID,
	)
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	defer func() { _ = rows.Close() }()

	versions := []map[string]any{}
	for rows.Next() {
		var (
			id, fileIDOut, hash string
			size                int64
			pushedBy, message   sql.NullString
			pushedAt            time.Time
			isCurrent           bool
		)
		if err := rows.Scan(&id, &fileIDOut, &hash, &size, &pushedBy, &pushedAt, &message, &isCurrent); err != nil {
			httpx.InternalError(w, r, err)
			return
		}
		versions = append(versions, map[string]any{
			"id":           id,
			"file_id":      fileIDOut,
			"content_hash": hash,
			"size":         size,
			"pushed_by":    pushedBy.String,
			"pushed_at":    pushedAt,
			"message":      message.String,
			"is_current":   isCurrent,
		})
	}
	if err := rows.Err(); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

// Moves is_current pointer to a prior version and mirrors the target
// version's content metadata onto the parent files row.
func (h *Handler) RollbackFile(w http.ResponseWriter, r *http.Request) {
	uid := httpx.UserID(r.Context())
	if uid == "" {
		httpx.RespondError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	envID := httpx.EnvironmentID(r.Context())
	role := httpx.WorkspaceRole(r.Context())
	fileID := r.PathValue("fileId")
	if fileID == "" {
		httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
		return
	}
	if role == "viewer" {
		httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "write permission required")
		return
	}

	var req rollbackRequest
	if err := httpx.Bind(r, &req); err != nil {
		httpx.RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid rollback request")
		return
	}

	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback() }()

	// Confirm the parent file lives in this env.
	var path string
	err = tx.QueryRowContext(r.Context(),
		`SELECT path FROM files WHERE id = $1 AND environment_id = $2`,
		fileID, envID,
	).Scan(&path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.RespondError(w, http.StatusForbidden, "FORBIDDEN", "access denied")
			return
		}
		httpx.InternalError(w, r, err)
		return
	}

	// Pull and lock the target version. FOR UPDATE serialises concurrent
	// rollbacks against the same target so the demote/promote sequence stays
	// consistent — without it two simultaneous rollbacks could race against
	// the partial unique index on is_current.
	var (
		targetHash     string
		targetSize     int64
		targetPushedBy sql.NullString
		targetPushedAt time.Time
	)
	err = tx.QueryRowContext(r.Context(),
		`SELECT content_hash, size, pushed_by, pushed_at
		   FROM file_versions
		  WHERE id = $1 AND file_id = $2
		  FOR UPDATE`,
		req.VersionID, fileID,
	).Scan(&targetHash, &targetSize, &targetPushedBy, &targetPushedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httpx.RespondError(w, http.StatusNotFound, "NOT_FOUND", "version not found")
			return
		}
		httpx.InternalError(w, r, err)
		return
	}

	// Demote before promoting. The partial unique index on
	// file_versions (file_id) WHERE is_current would reject the promote
	// otherwise.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE file_versions SET is_current = FALSE WHERE file_id = $1 AND is_current = TRUE`,
		fileID,
	); err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE file_versions SET is_current = TRUE WHERE id = $1`,
		req.VersionID,
	); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	// Mirror the target version's original metadata onto the parent row.
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE files
		    SET content_hash = $1, size = $2, pushed_by = $3, pushed_at = $4
		  WHERE id = $5`,
		targetHash, targetSize, targetPushedBy, targetPushedAt, fileID,
	); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	if err := tx.Commit(); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id":             fileID,
		"environment_id": envID,
		"path":           path,
		"content_hash":   targetHash,
		"size":           targetSize,
		"pushed_by":      targetPushedBy.String,
		"pushed_at":      targetPushedAt,
	})
}

// reject "..", absolute paths, null bytes, and leading separators.
func validateFilePath(p string) error {
	if p == "" {
		return errors.New("empty path")
	}
	if strings.ContainsRune(p, 0) {
		return errors.New("null byte in path")
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return errors.New("absolute path")
	}
	// Treat both separators as path-segment delimiters so Windows-style
	// inputs can't sneak ".." past the check.
	for _, sep := range []string{"/", "\\"} {
		for _, seg := range strings.Split(p, sep) {
			if seg == ".." {
				return errors.New("path traversal")
			}
		}
	}
	return nil
}

// validateSha256Hex requires exactly 64 lowercase hex chars. The lowercase
// requirement is part of the wire contract: the same bytes must always hash
// to the same blob key, so accepting mixed case would let two clients push
// the same file to two different blob keys.
func validateSha256Hex(s string) error {
	if len(s) != 64 {
		return errors.New("invalid hash length")
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return errors.New("invalid hash format")
		}
	}
	return nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
