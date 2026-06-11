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
		userHandler *handler.UserManagementHandler,
		deviceHandler *handler.DeviceHandler,
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
	mux.HandleFunc("/api/v1/dashboard/nodes/stream", middleware.RequireAuth(dashboardhandler.StreamNodes))
	mux.HandleFunc("/api/v1/dashboard/telemetry", middleware.RequireAuth(dashboardhandler.GetTelemetry))
	mux.HandleFunc("/api/v1/dashboard/telemetry/stream", middleware.RequireAuth(dashboardhandler.StreamTelemetry))
	mux.HandleFunc("/api/v1/dashboard/ota-logs", middleware.RequireAuth(dashboardhandler.GetOTALogs))
	mux.HandleFunc("/api/v1/dashboard/ota-logs/stream", middleware.RequireAuth(dashboardhandler.StreamOTALogs))
	
	// REQUIRE AUTH AND ADMIN ROLE //
	adminMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
        return middleware.RequireAuth(middleware.RequireRole("ADMIN", next))
    }

    // User Management (Hanya Admin yang bisa mengakses ini)
    mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
        switch r.Method {
        case http.MethodGet:
            adminMiddleware(userHandler.GetUsers)(w, r)
        case http.MethodDelete:
            adminMiddleware(userHandler.DeleteUser)(w, r)
        default:
            middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{Success: false, Message: "Method Not Allowed"})
        }
    })

	mux.HandleFunc("/api/v1/admin/gateways", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			adminMiddleware(deviceHandler.GetGateways)(w, r)
		case http.MethodPost:
			adminMiddleware(deviceHandler.CreateGateway)(w, r)
		case http.MethodPut:
			adminMiddleware(deviceHandler.UpdateGateway)(w, r)
		case http.MethodDelete:
			adminMiddleware(deviceHandler.DeleteGateway)(w, r)
    	}
	})

	mux.HandleFunc("/api/v1/admin/nodes", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			adminMiddleware(deviceHandler.CreateNode)(w, r)
		case http.MethodPut:
			adminMiddleware(deviceHandler.UpdateNode)(w, r)
		case http.MethodDelete:
			adminMiddleware(deviceHandler.DeleteNode)(w, r)
		}
	})
    
    mux.HandleFunc("/api/v1/admin/users/role", adminMiddleware(userHandler.UpdateUserRole))
    mux.HandleFunc("/api/v1/admin/users/revoke", adminMiddleware(userHandler.RevokeUserSessions))
	mux.HandleFunc("/api/v1/firmware/trigger", adminMiddleware(firmwareHandler.TriggerExistingOTA))
	mux.HandleFunc("/api/v1/firmware/bulk-trigger", adminMiddleware(firmwareHandler.TriggerBulkOTA))
	mux.HandleFunc("/api/v1/firmwares", adminMiddleware(firmwareHandler.GetAllFirmwares))
	mux.HandleFunc("/api/v1/firmware/delete", adminMiddleware(firmwareHandler.DeleteFirmware))

	mux.HandleFunc("/api/v1/firmware/upload", adminMiddleware(firmwareHandler.Upload))
	return middleware.EnableCORS(mux)
}