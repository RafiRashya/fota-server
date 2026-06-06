package middleware

import (
    "context"
    "encoding/json"
    "net/http"
    "os"
    "strings"

    "github.com/golang-jwt/jwt/v5"
)

type contextKey string
const UserRoleKey contextKey = "userRole"
const UserIDKey contextKey = "userID"

// JsonResponse untuk konsistensi response
type JsonResponse struct {
    Success bool        `json:"success"`
    Message string      `json:"message,omitempty"`
    Data    interface{} `json:"data,omitempty"`
}

func WriteJSON(w http.ResponseWriter, statusCode int, resp JsonResponse) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(statusCode)
    json.NewEncoder(w).Encode(resp)
}

// 1. Middleware Validasi Token
func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        authHeader := r.Header.Get("Authorization")
        var tokenString string

        if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
            tokenString = strings.TrimPrefix(authHeader, "Bearer ")
        } else {
            tokenString = r.URL.Query().Get("token")
        }

        if tokenString == "" {
            WriteJSON(w, http.StatusUnauthorized, JsonResponse{
                Success: false,
                Message: "Token tidak ditemukan",
            })
            return
        }

        secret := []byte(os.Getenv("JWT_SECRET"))

        // Parsing dan Validasi Token
        token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
            return secret, nil
        })

        if err != nil || !token.Valid {
            WriteJSON(w, http.StatusUnauthorized, JsonResponse{
                Success: false,
                Message: "Token tidak valid atau kadaluarsa",
            })
            return
        }

        // Ekstrak data dari dalam token (Role & ID)
        claims, ok := token.Claims.(jwt.MapClaims)
        if !ok {
            WriteJSON(w, http.StatusUnauthorized, JsonResponse{
                Success: false,
                Message: "Struktur klaim salah",
            })
            return
        }

        // Masukkan data pengguna ke dalam Context Request
        ctx := context.WithValue(r.Context(), UserRoleKey, claims["role"])
        ctx = context.WithValue(ctx, UserIDKey, claims["user_id"])
        
        next.ServeHTTP(w, r.WithContext(ctx))
    }
}

// 2. Middleware Pengecek Role
func RequireRole(allowedRole string, next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        role := r.Context().Value(UserRoleKey)
        if role == nil || role.(string) != allowedRole {
            WriteJSON(w, http.StatusForbidden, JsonResponse{
                Success: false,
                Message: "Akses ditolak: anda tidak memiliki hak akses",
            })
            return
        }
        next.ServeHTTP(w, r)
    }
}