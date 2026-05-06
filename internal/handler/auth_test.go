package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newTestRouter(t *testing.T) (*gin.Engine, sqlmock.Sqlmock) {
	t.Helper()
	mockDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { mockDB.Close() })

	h := New(mockDB)
	r := gin.New()
	r.POST("/auth/register", h.Register)
	return r, mock
}

func doJSON(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRegisterSuccess(t *testing.T) {
	r, mock := newTestRouter(t)

	mock.ExpectExec("INSERT INTO users").
		WithArgs(sqlmock.AnyArg(), "alice@example.com", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	w := doJSON(r, "POST", "/auth/register",
		`{"email":"Alice@Example.com","password":"correct horse battery staple"}`)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(resp.User.ID, "usr_") {
		t.Errorf("id = %q, want usr_ prefix", resp.User.ID)
	}
	if resp.User.Email != "alice@example.com" {
		t.Errorf("email = %q, want lowercased", resp.User.Email)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock: %v", err)
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	r, mock := newTestRouter(t)

	mock.ExpectExec("INSERT INTO users").
		WillReturnError(&pq.Error{Code: "23505"})

	w := doJSON(r, "POST", "/auth/register",
		`{"email":"taken@example.com","password":"correct horse battery staple"}`)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code string `json:"code"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Code != "REGISTRATION_FAILED" {
		t.Errorf("code = %q, want REGISTRATION_FAILED", resp.Code)
	}
}

func TestRegisterMalformedJSON(t *testing.T) {
	r, _ := newTestRouter(t)

	w := doJSON(r, "POST", "/auth/register", `{not json`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRegisterPasswordTooLong(t *testing.T) {
	r, _ := newTestRouter(t)

	pw := strings.Repeat("a", 73)
	body := `{"email":"a@b.com","password":"` + pw + `"}`
	w := doJSON(r, "POST", "/auth/register", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestRegisterInvalidEmail(t *testing.T) {
	r, _ := newTestRouter(t)

	w := doJSON(r, "POST", "/auth/register",
		`{"email":"not-an-email","password":"correct horse battery staple"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
