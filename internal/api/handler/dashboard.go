package handler

import (
	"net/http"
	"strconv"

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
		Data: nodes,
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
		Data: telemetry,
	})
}

// ================= MENDAPATKAN RIWAYAT FOTA =================
func (h *DashboardHandler) GetOTALogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
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
		http.Error(w, "Gagal mengambil riwayat OTA", http.StatusInternalServerError)
		return
	}

	for i := range logs {
		logs[i].Admin.PasswordHash = ""
	}

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Data: logs,
	})
}