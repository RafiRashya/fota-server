package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"time"

	"fota-backend/internal/middleware"
	"fota-backend/internal/models"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AuthHandler struct{
	DB *gorm.DB
}

func NewAuthHandler(db *gorm.DB) *AuthHandler{
	return &AuthHandler{DB: db}
}

type RegisterReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"` // ADMIN atau USER
}

type LoginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UserResponse struct {
    ID    string `json:"id"`
    Email string `json:"email"`
    Role  string `json:"role"`
}

type LoginResponse struct {
    Token string       `json:"token"`
    User  UserResponse `json:"user"`
}

// Validasi email format
func isValidEmail(email string) bool {
    re := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
    return re.MatchString(email)
}

// Validasi password strength
func isStrongPassword(password string) bool {
    return len(password) >= 6
}

// ================= REGISTRASI =================
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
			Success: false,
			Message: "Method Not Allowed",
		})
		return
	}

	var req RegisterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "Invalid request format",
		})
		return
	}

	if !isValidEmail(req.Email){
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "Invalid Email Format",
		})
		return
	}

	if !isStrongPassword(req.Password){
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "Password must contain at least 6 characters",
		})
		return
	}

	if req.Role != "ADMIN" && req.Role != "USER"{
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "Role must be ADMIN or User",
		})
	}

	// 1. Enkripsi Kata Sandi (Bcrypt)
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{
            Success: false,
            Message: "Fail to hash Password",
        })
		return
	}

	// 2. Simpan ke Database
	user := models.User{
		Email:        req.Email,
		PasswordHash: string(hashedPassword),
		Role:         req.Role,
	}

	if err := h.DB.Create(&user).Error; err != nil {
		middleware.WriteJSON(w, http.StatusConflict, middleware.JsonResponse{
            Success: false,
            Message: "Email already registered",
        })
		return
	}

	middleware.WriteJSON(w, http.StatusCreated, middleware.JsonResponse{
        Success: true,
        Message: "Registrasi berhasil",
    })
}

// ================= LOGIN =================
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
			Success: false,
			Message: "Method Not Allowed",
		})
		return
	}

	var req LoginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
            Success: false,
            Message: "Invalid request format",
        })
		return
	}

	// 1. Cari User berdasarkan Email
	var user models.User
	if err := h.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		 middleware.WriteJSON(w, http.StatusUnauthorized, middleware.JsonResponse{
            Success: false,
            Message: "Wrong Email or Password",
        })
		return
	}

	// 2. Cocokkan Kata Sandi
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		middleware.WriteJSON(w, http.StatusUnauthorized, middleware.JsonResponse{
            Success: false,
            Message: "Wrong Email or Password",
        })
		return
	}

	// 3. Buat Token JWT
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": user.ID.String(),
		"role":    user.Role,
		"exp":     time.Now().Add(24 * time.Hour).Unix(), // Token mati dalam 24 Jam
	})

	tokenString, err := token.SignedString([]byte(os.Getenv("JWT_SECRET")))
	if err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{
            Success: false,
            Message: "Fail to create a token",
        })
		return
	}

	// 4. Kirim Token sebagai JSON (untuk Mobile/Postman)
	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
        Success: true,
        Message: "Login berhasil",
        Data: LoginResponse{
            Token: tokenString,
            User: UserResponse{
                ID:    user.ID.String(),
                Email: user.Email,
                Role:  user.Role,
            },
        },
    })
}

// ================= LOGOUT =================
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
            Success: false,
            Message: "Method not allowed",
        })
        return
    }

    middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
        Success: true,
        Message: "Successfully Logout, remove token from client side",
    })
}

func (h *AuthHandler) GetMe(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
            Success: false,
            Message: "Method not allowed",
        })
        return
    }

    // Extract user ID from context (dari middleware RequireAuth)
    userIDStr, ok := r.Context().Value(middleware.UserIDKey).(string)
    if !ok {
        middleware.WriteJSON(w, http.StatusUnauthorized, middleware.JsonResponse{
            Success: false,
            Message: "Couldn't find User Id",
        })
        return
    }

    userID, err := uuid.Parse(userIDStr)
    if err != nil {
        middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
            Success: false,
            Message: "Invalid User Id format",
        })
        return
    }

    // Ambil data user
    var user models.User
    if err := h.DB.First(&user, userID).Error; err != nil {
        middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{
            Success: false,
            Message: "Couldn't find user",
        })
        return
    }

    middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
        Success: true,
        Data: UserResponse{
            ID:    user.ID.String(),
            Email: user.Email,
            Role:  user.Role,
        },
    })
}