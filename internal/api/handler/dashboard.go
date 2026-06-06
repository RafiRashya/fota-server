package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"fota-backend/internal/middleware"
	"fota-backend/internal/models"

	"gorm.io/gorm"
)

type DashboardHandler struct {
	DB *gorm.DB
}

func NewDashboardHandler(db *gorm.DB) *DashboardHandler {
	return &DashboardHandler{DB: db}
}

// ================= MENDAPATKAN DAFTAR NODE =================
// Berguna untuk mengisi Dropdown/Pilihan Sensor di Dashboard
func (h *DashboardHandler) GetNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
			Success: false,
			Message: "Method Not Allowed",
		})
		return
	}

	var nodes []models.Node
	// Preload digunakan agar data firmware yang sedang dipakai ikut terbawa dalam JSON
	if err := h.DB.Preload("CurrentFirmware").Find(&nodes).Error; err != nil {
		middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{
			Success: false,
			Message: "Failed to fetch Nodes",
		})
		return
	}

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Data:    nodes,
	})
}

// ================= MENDAPATKAN DATA GRAFIK TELEMETRI =================
func (h *DashboardHandler) GetTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
			Success: false,
			Message: "Method Not Allowed",
		})
		return
	}

	nodeMAC := r.URL.Query().Get("node_mac")
	if nodeMAC == "" {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "node_mac parameter cannot be empty",
		})
		return
	}

	// Batasi jumlah data yang ditarik agar browser tidak crash (default 50 data terakhir)
	limit := 50
	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	// 1. Cari Node ID
	var node models.Node
	if err := h.DB.Where("mac_address = ?", nodeMAC).First(&node).Error; err != nil {
		middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{
			Success: false,
			Message: "Couldn't find Node",
		})
		return
	}

	// 2. Tarik data dari TimescaleDB (Ambil 'limit' data terbaru)
	var telemetry []models.ShmTelemetry
	if err := h.DB.Where("node_id = ?", node.ID).Order("time desc").Limit(limit).Find(&telemetry).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{
			Success: false,
			Message: "Failed to fetch telemetry data",
		})
		return
	}

	// 3. Balik urutan Array (Reverse)
	for i, j := 0, len(telemetry)-1; i < j; i, j = i+1, j-1 {
		telemetry[i], telemetry[j] = telemetry[j], telemetry[i]
	}

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Data:    telemetry,
	})
}

// ================= MENDAPATKAN RIWAYAT FOTA =================
func (h *DashboardHandler) GetOTALogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
			Success: false,
			Message: "Method Not Allowed",
		})
		return
	}

	var logs []models.OtaLog

	err := h.DB.
		Preload("Node").
		Preload("TargetFirmware").
		Preload("Admin").
		Order("started_at desc").
		Limit(100).
		Find(&logs).Error

	if err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{
			Success: false,
			Message: "Failed to load OTA logs",
		})
		return
	}

	for i := range logs {
		logs[i].Admin.PasswordHash = ""
	}

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Data:    logs,
	})
}

// ================= STREAM DATA NODE (SSE) =================
func (h *DashboardHandler) StreamNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
			Success: false,
			Message: "Method Not Allowed",
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	sendNodesData := func() error {
		var nodes []models.Node
		if err := h.DB.Preload("CurrentFirmware").Find(&nodes).Error; err != nil {
			return err
		}

		jsonData, err := json.Marshal(middleware.JsonResponse{
			Success: true,
			Data:    nodes,
		})
		if err != nil {
			return err
		}

		_, err = fmt.Fprintf(w, "data: %s\n\n", jsonData)
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if err := sendNodesData(); err != nil {
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if err := sendNodesData(); err != nil {
				return
			}
		}
	}
}

// ================= STREAM RIWAYAT FOTA (SSE) =================
func (h *DashboardHandler) StreamOTALogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
			Success: false,
			Message: "Method Not Allowed",
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	sendOTALogsData := func() error {
		var logs []models.OtaLog
		err := h.DB.
			Preload("Node").
			Preload("TargetFirmware").
			Preload("Admin").
			Order("started_at desc").
			Limit(100).
			Find(&logs).Error
		if err != nil {
			return err
		}

		for i := range logs {
			logs[i].Admin.PasswordHash = ""
		}

		jsonData, err := json.Marshal(middleware.JsonResponse{
			Success: true,
			Data:    logs,
		})
		if err != nil {
			return err
		}

		_, err = fmt.Fprintf(w, "data: %s\n\n", jsonData)
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if err := sendOTALogsData(); err != nil {
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if err := sendOTALogsData(); err != nil {
				return
			}
		}
	}
}

// ================= STREAM DATA TELEMETRI (SSE) =================
func (h *DashboardHandler) StreamTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
			Success: false,
			Message: "Method Not Allowed",
		})
		return
	}

	nodeMAC := r.URL.Query().Get("node_mac")
	if nodeMAC == "" {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "node_mac parameter cannot be empty",
		})
		return
	}

	limit := 100
	limitStr := r.URL.Query().Get("limit")
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// 1. Cari Node ID
	var node models.Node
	if err := h.DB.Where("mac_address = ?", nodeMAC).First(&node).Error; err != nil {
		middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{
			Success: false,
			Message: "Couldn't find Node",
		})
		return
	}

	sendTelemetryData := func() error {
		var telemetry []models.ShmTelemetry
		if err := h.DB.Where("node_id = ?", node.ID).Order("time desc").Limit(limit).Find(&telemetry).Error; err != nil {
			return err
		}

		// Balik urutan Array (Reverse)
		for i, j := 0, len(telemetry)-1; i < j; i, j = i+1, j-1 {
			telemetry[i], telemetry[j] = telemetry[j], telemetry[i]
		}

		jsonData, err := json.Marshal(middleware.JsonResponse{
			Success: true,
			Data:    telemetry,
		})
		if err != nil {
			return err
		}

		_, err = fmt.Fprintf(w, "data: %s\n\n", jsonData)
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if err := sendTelemetryData(); err != nil {
		return
	}

	// Stream telemetry updates every 3 seconds
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if err := sendTelemetryData(); err != nil {
				return
			}
		}
	}
}