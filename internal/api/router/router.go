package router

import (
	"fota-backend/internal/api/handler"
	"net/http"
)

func SetupRouter(firmwareHandler *handler.FirmwareHandler) *http.ServeMux{
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/firmware/upload", firmwareHandler.Upload)
	return mux
}