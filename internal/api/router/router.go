package router

import (
	"fota-backend/internal/api/handler"
	"fota-backend/internal/middleware"
	"net/http"

)

func SetupRouter(firmwareHandler *handler.FirmwareHandler, authHandler *handler.AuthHandler) *http.ServeMux{
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/auth/register", authHandler.Register)
	mux.HandleFunc("/api/v1/auth/login", authHandler.Login)
	mux.HandleFunc("/api/v1/auth/logout", authHandler.Logout)
	mux.HandleFunc("/api/v1/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("/api/v1/auth/me", middleware.RequireAuth(authHandler.GetMe))

	uploadEndpoint := middleware.RequireAuth(
		middleware.RequireRole("ADMIN", firmwareHandler.Upload),
	)

	mux.HandleFunc("/api/v1/firmware/upload", uploadEndpoint)
	return mux
}