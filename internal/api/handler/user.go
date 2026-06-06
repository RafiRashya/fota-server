package handler

import (
	"encoding/json"
	"net/http"

	"fota-backend/internal/middleware"
	"fota-backend/internal/models"
	"gorm.io/gorm"
)

type UserManagementHandler struct {
	DB *gorm.DB
}

func NewUserManagementHandler(db *gorm.DB) *UserManagementHandler {
	return &UserManagementHandler{DB: db}
}

// Struct untuk request body update role
type UpdateRoleReq struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// Struct untuk request body revoke session
type RevokeSessionReq struct {
	UserID string `json:"user_id"`
}

// ================= 1. MENDAPATKAN DAFTAR PENGGUNA (READ) =================
func (h *UserManagementHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{Success: false, Message: "Method Not Allowed"})
		return
	}

	var users []models.User
	if err := h.DB.Order("created_at desc").Find(&users).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal mengambil data pengguna"})
		return
	}

	// Gunakan struct UserResponse dari auth.go agar PasswordHash tidak bocor ke Frontend
	var usersResp []UserResponse
	for _, u := range users {
		usersResp = append(usersResp, UserResponse{
			ID:    u.ID.String(),
			Email: u.Email,
			Role:  u.Role,
		})
	}

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Data:    usersResp,
	})
}

// ================= 2. MENGUBAH ROLE PENGGUNA (UPDATE) =================
func (h *UserManagementHandler) UpdateUserRole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{Success: false, Message: "Method Not Allowed"})
		return
	}

	var req UpdateRoleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "Format request tidak valid"})
		return
	}

	if req.Role != "ADMIN" && req.Role != "USER" {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "Role hanya boleh ADMIN atau USER"})
		return
	}

	var user models.User
	if err := h.DB.Where("id = ?", req.UserID).First(&user).Error; err != nil {
		middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{Success: false, Message: "Pengguna tidak ditemukan"})
		return
	}

	// Update role di database
	if err := h.DB.Model(&user).Update("role", req.Role).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal mengupdate role"})
		return
	}

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Message: "Role pengguna berhasil diperbarui menjadi " + req.Role,
	})
}

// ================= 3. MENGHAPUS PENGGUNA (DELETE) =================
func (h *UserManagementHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodDelete {
        middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
            Success: false,
            Message: "Method Not Allowed",
        })
        return
    }

    userID := r.URL.Query().Get("id")
    if userID == "" {
        middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
            Success: false,
            Message: "Parameter ID tidak boleh kosong",
        })
        return
    }

    err := h.DB.Transaction(func(tx *gorm.DB) error {
        if err := tx.Where("user_id = ?", userID).Delete(&models.RefreshToken{}).Error; err != nil {
            return err
        }

        result := tx.Where("id = ?", userID).Delete(&models.User{})
        if result.Error != nil {
            return result.Error
        }

        if result.RowsAffected == 0 {
            return gorm.ErrRecordNotFound
        }

        return nil
    })

    if err != nil {
        if err == gorm.ErrRecordNotFound {
            middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{
                Success: false,
                Message: "Pengguna tidak ditemukan",
            })
            return
        }

        middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{
            Success: false,
            Message: "Gagal menghapus pengguna atau sesi token terkait",
        })
        return
    }

    middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
        Success: true,
        Message: "Pengguna dan seluruh token login terkait berhasil dihapus",
    })
}

// ================= 4. MENCABUT SESI PENGGUNA (REVOKE) =================
// Berguna jika ada akun yang diretas, Admin bisa langsung "menendang" mereka keluar
func (h *UserManagementHandler) RevokeUserSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{Success: false, Message: "Method Not Allowed"})
		return
	}

	var req RevokeSessionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "Format request tidak valid"})
		return
	}

	// Ubah semua Refresh Token milik pengguna ini menjadi revoked = true
	if err := h.DB.Model(&models.RefreshToken{}).Where("user_id = ?", req.UserID).Update("revoked", true).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal mencabut sesi pengguna"})
		return
	}

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Message: "Seluruh sesi pengguna ini berhasil diputus (Force Logout)",
	})
}