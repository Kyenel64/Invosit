package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/kyenel64/invosit-api/internal/httpx"
	"github.com/kyenel64/invosit-api/internal/ids"
)

type createWorkspaceRequest struct {
	Name string `json:"name" validate:"required,min=1,max=64"`
}

func (h *Handler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	uid := httpx.UserID(r.Context())
	if uid == "" {
		httpx.RespondError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}

	var req createWorkspaceRequest
	if err := httpx.Bind(r, &req); err != nil {
		httpx.RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid workspace name")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		httpx.RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid workspace name")
		return
	}

	workspaceId := ids.Workspace()
	createdAt := time.Now().UTC()

	transaction, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	defer transaction.Rollback()

	if _, err := transaction.ExecContext(r.Context(),
		`INSERT INTO workspaces(id, name, created_by, created_at) VALUES($1, $2, $3, $4)`,
		workspaceId, name, uid, createdAt,
	); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	if _, err := transaction.ExecContext(r.Context(),
		`INSERT INTO workspace_members(workspace_id, user_id, role, joined_at) VALUES($1, $2, 'admin', $3)`,
		workspaceId, uid, createdAt,
	); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	if err := transaction.Commit(); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"id":         workspaceId,
		"name":       name,
		"created_by": uid,
		"created_at": createdAt,
	})
}

func (h *Handler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	uid := httpx.UserID(r.Context())
	if uid == "" {
		httpx.RespondError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}

	rows, err := h.db.QueryContext(r.Context(),
		`SELECT w.id, w.name, w.created_by, w.created_at, m.role
		FROM workspaces w
		JOIN workspaces_members m ON m.workspace_id = w.id
		WHERE m.user_id = $1
		AND (m.expires_at IS NULL OR m.expires_at > NOW())
		ORDER BY w.created_at DESC`,
		uid,
	)
	if err != nil {
		httpx.InternalError(w, r, err)
		return
	}
	defer rows.Close()

	workspaces := []map[string]any{}
	for rows.Next() {
		var (
			id, name, createdBy, role string
			createdAt                 time.Time
		)
		if err := rows.Scan(&id, &name, &createdBy, &createdAt, &role); err != nil {
			httpx.InternalError(w, r, err)
			return
		}
		workspaces = append(workspaces, map[string]any{
			"id":         id,
			"name":       name,
			"created_by": createdBy,
			"created_at": createdAt,
			"role":       role,
		})
	}
	if err := rows.Err(); err != nil {
		httpx.InternalError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"workspaces": workspaces})
}
