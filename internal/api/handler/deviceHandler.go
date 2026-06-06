package handler

import (
	"encoding/json"
	"net/http"
	"log"       
	"strings" 
	"time"

	"fota-backend/internal/middleware"
	"fota-backend/internal/models"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type DeviceHandler struct {
	DB *gorm.DB
}

func NewDeviceHandler(db *gorm.DB) *DeviceHandler {
	return &DeviceHandler{DB: db}
}

// Struct untuk request Gateway
type CreateGatewayReq struct {
	Name       string `json:"name"`
	MacAddress string `json:"mac_address"`
	Location   string `json:"location"`
}

// Struct untuk request Node
type CreateNodeReq struct {
	GatewayID  string `json:"gateway_id"`
	Name       string `json:"name"`
	MacAddress string `json:"mac_address"`
}

type UpdateDeviceReq struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Location string `json:"location"`
}

type GatewayStatusPayload struct {
	GatewayMAC string `json:"gateway_mac"`
	Status     string `json:"status"`
}

func (h *DeviceHandler) HandleGatewayStatus(client pahomqtt.Client, msg pahomqtt.Message) {
	var payload GatewayStatusPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Printf("[GATEWAY-STATUS] Gagal parse JSON: %v", err)
		return
	}

	currentTime := time.Now()
	// Ubah status jadi huruf kapital (ONLINE/OFFLINE) agar seragam dengan DB
	status := strings.ToUpper(payload.Status) 

	// 1. Update status Gateway di tabel
	result := h.DB.Model(&models.Gateway{}).
		Where("mac_address = ?", payload.GatewayMAC).
		Updates(map[string]interface{}{
			"status":    status,
			"last_seen": currentTime,
		})

	if result.RowsAffected == 0 {
		log.Printf("[GATEWAY-STATUS] Mengabaikan data: Gateway %s tidak terdaftar", payload.GatewayMAC)
		return
	}

	log.Printf("[GATEWAY-STATUS] Gateway %s berstatus %s", payload.GatewayMAC, status)

	// 2. Jika Gateway mati (OFFLINE), putus juga semua Node yang menempel padanya
	if status == "OFFLINE" {
		var gateway models.Gateway
		h.DB.Where("mac_address = ?", payload.GatewayMAC).First(&gateway)
		
		h.DB.Model(&models.Node{}).
			Where("gateway_id = ?", gateway.ID).
			Update("status", "OFFLINE")
			
		log.Printf("[GATEWAY-STATUS] Semua Node pada Gateway %s ikut di-set OFFLINE", payload.GatewayMAC)
	}
}

// ================= 1. GET ALL GATEWAYS =================
func (h *DeviceHandler) GetGateways(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{Success: false, Message: "Method Not Allowed"})
		return
	}

	var gateways []models.Gateway
	// Mengambil Gateway sekaligus menghitung jumlah Node yang terhubung ke Gateway tersebut
	if err := h.DB.Preload("Nodes").Find(&gateways).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal mengambil data gateway"})
		return
	}

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Data:    gateways,
	})
}

// ================= 2. CREATE GATEWAY =================
func (h *DeviceHandler) CreateGateway(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{Success: false, Message: "Method Not Allowed"})
		return
	}

	var req CreateGatewayReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "Format request tidak valid"})
		return
	}

	// Validasi input kosong
	if req.Name == "" || req.MacAddress == "" {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "Nama dan MAC Address tidak boleh kosong"})
		return
	}

	gateway := models.Gateway{
		Name:       req.Name,
		MacAddress: req.MacAddress,
		Location:   &req.Location,
	}

	if err := h.DB.Create(&gateway).Error; err != nil {
		middleware.WriteJSON(w, http.StatusConflict, middleware.JsonResponse{Success: false, Message: "Gagal menyimpan: MAC Address mungkin sudah terdaftar"})
		return
	}

	middleware.WriteJSON(w, http.StatusCreated, middleware.JsonResponse{Success: true, Message: "Gateway berhasil ditambahkan"})
}

func (h *DeviceHandler) UpdateGateway(w http.ResponseWriter, r *http.Request) {
	var req UpdateDeviceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "Invalid request body"})
		return
	}

	if err := h.DB.Model(&models.Gateway{}).Where("id = ?", req.ID).Updates(map[string]interface{}{
		"name":     req.Name,
		"location": req.Location,
	}).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal memperbarui gateway"})
		return
	}
	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{Success: true, Message: "Data Gateway berhasil diperbarui"})
}

func (h *DeviceHandler) DeleteGateway(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	// Catatan: Menghapus Gateway akan gagal jika masih ada Node yang terikat (Integritas Database)
	if err := h.DB.Delete(&models.Gateway{}, "id = ?", id).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal menghapus Gateway. Pastikan tidak ada Node yang terikat."})
		return
	}
	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{Success: true, Message: "Gateway berhasil dihapus"})
}

// ================= 3. CREATE NODE =================
func (h *DeviceHandler) CreateNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{Success: false, Message: "Method Not Allowed"})
		return
	}

	var req CreateNodeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "Format request tidak valid"})
		return
	}

	gatewayID, err := uuid.Parse(req.GatewayID)
	if err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "Format Gateway ID tidak valid"})
		return
	}

	var defaultFw models.Firmware
	var firmwareID *uuid.UUID
	if err := h.DB.Where("version = ?", "1.0.0").First(&defaultFw).Error; err == nil {
		firmwareID = &defaultFw.ID
	}

	node := models.Node{
		GatewayID:         gatewayID,
		Name:              req.Name,
		MacAddress:        req.MacAddress,
		CurrentFirmwareID: firmwareID,
	}

	if err := h.DB.Create(&node).Error; err != nil {
		middleware.WriteJSON(w, http.StatusConflict, middleware.JsonResponse{Success: false, Message: "Gagal menyimpan: MAC Address Node mungkin sudah terdaftar"})
		return
	}

	middleware.WriteJSON(w, http.StatusCreated, middleware.JsonResponse{Success: true, Message: "Node berhasil ditambahkan"})
}

func (h *DeviceHandler) UpdateNode(w http.ResponseWriter, r *http.Request) {
	var req UpdateDeviceReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "Invalid request body"})
		return
	}

	if err := h.DB.Model(&models.Node{}).Where("id = ?", req.ID).Update("name", req.Name).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal memperbarui nama node"})
		return
	}
	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{Success: true, Message: "Nama Node berhasil diperbarui"})
}

func (h *DeviceHandler) DeleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	
	if id == "" {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "ID Node wajib disertakan"})
		return
	}

	// 1. TAMBAHAN: Hapus semua OTA Logs yang terkait dengan Node ini terlebih dahulu
	if err := h.DB.Where("node_id = ?", id).Delete(&models.OtaLog{}).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal menghapus riwayat OTA milik Node ini"})
		return
	}

	// 2. KODE ASLI: Setelah log bersih, baru hapus Node-nya
	if err := h.DB.Delete(&models.Node{}, "id = ?", id).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal menghapus Node"})
		return
	}
    
	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{Success: true, Message: "Node beserta riwayatnya berhasil dihapus"})
}