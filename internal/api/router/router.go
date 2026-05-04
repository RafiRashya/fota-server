package router

import (
	"fota-backend/internal/api/handler"
	"fota-backend/internal/middleware"
	"net/http"

)

func SetupRouter(
		firmwareHandler *handler.FirmwareHandler,
		authHandler *handler.AuthHandler,
		dashboardhandler *handler.DashboardHandler,
) http.Handler{
	mux := http.NewServeMux()
	
	// PUBLIC ROUTE //
	mux.HandleFunc("/api/v1/auth/register", authHandler.Register)
	mux.HandleFunc("/api/v1/auth/login", authHandler.Login)
	mux.HandleFunc("/api/v1/auth/logout", authHandler.Logout)
	mux.HandleFunc("/api/v1/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("/api/v1/auth/me", middleware.RequireAuth(authHandler.GetMe))

	// REQUIRE AUTH ROUTE //
	mux.HandleFunc("/api/v1/dashboard/nodes", middleware.RequireAuth(dashboardhandler.GetNodes))
	mux.HandleFunc("/api/v1/dashboard/telemetry", middleware.RequireAuth(dashboardhandler.GetTelemetry))
	mux.HandleFunc("/api/v1/dashboard/ota-logs", middleware.RequireAuth(dashboardhandler.GetOTALogs))
	
	// REQUIRE AUTH AND ADMIN ROLE //
	uploadEndpoint := middleware.RequireAuth(
		middleware.RequireRole("ADMIN", firmwareHandler.Upload),
	)

	mux.HandleFunc("/api/v1/firmware/upload", uploadEndpoint)
	return middleware.EnableCORS(mux)
}