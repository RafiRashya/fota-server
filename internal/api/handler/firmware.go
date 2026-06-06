package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"fota-backend/internal/middleware"
	"fota-backend/internal/models"
	"fota-backend/internal/mqtt"
	"io"
	"net/http"
	"time"
	"crypto/sha256"   // <--- TAMBAHAN
	"encoding/hex"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FirmwareHandler struct {
	StorageClient	*storage.Client
	BucketName	string
	GoogleAccessID	string
	PrivateKey	[]byte
	MQTTClient	*mqtt.MQTTClient
	Database	*gorm.DB
}

type TriggerOTAReq struct {
	NodeID     string `json:"node_id"`
	FirmwareID string `json:"firmware_id"`
}

func NewFirmwareHandler(client *storage.Client, bucket string, accessID string, privKey []byte, mqtt *mqtt.MQTTClient, db *gorm.DB) *FirmwareHandler {
	return &FirmwareHandler{
		StorageClient: client,
		BucketName: bucket,
		GoogleAccessID: accessID,
		PrivateKey: privKey,
		MQTTClient: mqtt,
		Database: db,
	}
}

func (h *FirmwareHandler) Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
			Success: false,
			Message: "Method Not Allowed",
		})
		return
	}

	// PERBAIKAN 1: ParseMultipartForm dipanggil PALING PERTAMA (Batas 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "Gagal memproses form data atau ukuran file terlalu besar",
		})
		return
	}

	version := r.FormValue("version")
	nodeName := r.FormValue("node_name")
	releaseNotes := r.FormValue("release_notes")

	if version == "" || nodeName == "" {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "Version and Node Name parameters cannot be empty",
		})
		return
	}

	var releaseNotesPtr *string
	if releaseNotes != "" {
		releaseNotesPtr = &releaseNotes
	}

	file, header, err := r.FormFile("firmware")
	if err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "Fail to parse firmware file",
		})
		return
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{
			Success: false,
			Message: "Fail when processing file integrity",
		})
		return
	}

	calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))
	file.Seek(0, io.SeekStart)

	var node models.Node
	if err := h.Database.Where("name = ?", nodeName).First(&node).Error; err != nil {
		middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{
			Success: false,
			Message: "Can't Find Node",
		})
		return
	}

	ctx := context.Background()
	objectKey := fmt.Sprintf("firmware/v%s/nimble-shm-ota.bin", version)

	bucket := h.StorageClient.Bucket(h.BucketName)
	obj := bucket.Object(objectKey)
	writer := obj.NewWriter(ctx)

	if _, err := io.Copy(writer, file); err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{
			Success: false,
			Message: "Fail Uploading firmware to Cloud Storage",
		})
		return
	}
	if err := writer.Close(); err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{
			Success: false,
			Message: "Fail to finishing the Upload",
		})
		return
	}

	opts := &storage.SignedURLOptions{
		GoogleAccessID: h.GoogleAccessID,
		PrivateKey:     h.PrivateKey,
		Method:         http.MethodGet,
		Expires:        time.Now().Add(15 * time.Minute),
	}

	signedURL, err := storage.SignedURL(h.BucketName, objectKey, opts)
	if err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{
			Success: false,
			Message: "Fail to create Signed URL",
		})
		return
	}

	// PERBAIKAN 2: Tangkap Error saat Insert Firmware ke Database
	newFirmware := models.Firmware{
		Version:      version,
		FileName:     header.Filename,
		FileSize:     int(header.Size),
		Checksum:     calculatedChecksum,
		ReleaseNotes: releaseNotesPtr,
	}
	if err := h.Database.Create(&newFirmware).Error; err != nil {
		// Jika gagal (misal versi sudah ada), beri tahu frontend!
		middleware.WriteJSON(w, http.StatusConflict, middleware.JsonResponse{
			Success: false,
			Message: "Gagal menyimpan data firmware. Pastikan versi belum pernah digunakan.",
		})
		return
	}

	userIDStr, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		middleware.WriteJSON(w, http.StatusUnauthorized, middleware.JsonResponse{
			Success: false,
			Message: "Fail to obtain Identity From Token",
		})
		return
	}

	adminId, err := uuid.Parse(userIDStr)
	if err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "Invalid Admin Id Format",
		})
		return
	}

	// PERBAIKAN 3: Tangkap Error saat Insert OTA Log
	otaLog := models.OtaLog{
		NodeID:           node.ID,
		TargetFirmwareID: newFirmware.ID,
		Status:           "PENDING",
		TriggeredBy:      adminId,
		StartedAt:        time.Now(),
	}
	if err := h.Database.Create(&otaLog).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{
			Success: false,
			Message: "Gagal membuat tiket log OTA",
		})
		return
	}

	// Jika semua aman, baru kita tembak ke MQTT!
	triggerMsg := map[string]string{
		"cmd":            "start_ota",
		"target_mac":     node.MacAddress,
		"url":            signedURL,
		"target_version": version,
	}

	payload, _ := json.Marshal(triggerMsg)
	if h.MQTTClient != nil {
		h.MQTTClient.Publish("shm/ota/trigger", 1, false, payload)
	}

	middleware.WriteJSON(w, http.StatusCreated, middleware.JsonResponse{
		Success: true,
		Message: fmt.Sprintf("Successfully upload %s Firmware file and publishing update trigger", header.Filename),
	})
}

func (h *FirmwareHandler) TriggerExistingOTA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{Success: false, Message: "Method Not Allowed"})
		return
	}

	var req TriggerOTAReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "Format request tidak valid"})
		return
	}

	// 1. Cari data Node
	var node models.Node
	if err := h.Database.First(&node, "id = ?", req.NodeID).Error; err != nil {
		middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{Success: false, Message: "Node tidak ditemukan"})
		return
	}

	// 2. Cari data Firmware yang sudah ada di DB
	var firmware models.Firmware
	if err := h.Database.First(&firmware, "id = ?", req.FirmwareID).Error; err != nil {
		middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{Success: false, Message: "Firmware tidak ditemukan di sistem"})
		return
	}

	// 3. Generate Signed URL baru dari GCS menggunakan Path Global yang baru
	objectKey := fmt.Sprintf("firmware/v%s/nimble-shm-ota.bin", firmware.Version)

	opts := &storage.SignedURLOptions{
		GoogleAccessID: h.GoogleAccessID,
		PrivateKey:     h.PrivateKey,
		Method:         http.MethodGet,
		Expires:        time.Now().Add(15 * time.Minute), // Berlaku 15 Menit
	}

	signedURL, err := storage.SignedURL(h.BucketName, objectKey, opts)
	if err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal membuat akses URL ke Cloud Storage"})
		return
	}

	// 4. Ambil Admin ID dari JWT Token
	userIDStr, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		middleware.WriteJSON(w, http.StatusUnauthorized, middleware.JsonResponse{Success: false, Message: "Unauthorized"})
		return
	}
	adminId, _ := uuid.Parse(userIDStr)

	// 5. Buat tiket/log OTA baru berstatus PENDING
	otaLog := models.OtaLog{
		NodeID:           node.ID,
		TargetFirmwareID: firmware.ID,
		Status:           "PENDING",
		TriggeredBy:      adminId,
		StartedAt:        time.Now(),
	}
	if err := h.Database.Create(&otaLog).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal membuat tiket log OTA"})
		return
	}

	// 6. Tembak trigger ke MQTT
	triggerMsg := map[string]string{
		"cmd":            "start_ota",
		"target_mac":     node.MacAddress,
		"url":            signedURL,
		"target_version": firmware.Version,
	}

	payload, _ := json.Marshal(triggerMsg)
	if h.MQTTClient != nil {
		h.MQTTClient.Publish("shm/ota/trigger", 1, false, payload)
	}

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Message: fmt.Sprintf("Berhasil memicu proses OTA versi %s untuk Node %s", firmware.Version, node.Name),
	})
}

// ================= 3. GET ALL FIRMWARES =================
func (h *FirmwareHandler) GetAllFirmwares(w http.ResponseWriter, r *http.Request) {
	// Pastikan hanya menerima request GET
	if r.Method != http.MethodGet {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{
			Success: false, 
			Message: "Method Not Allowed",
		})
		return
	}

	var firmwares []models.Firmware
	
	// Ambil semua data firmware dari database, urutkan dari yang paling baru diunggah (descending)
	if err := h.Database.Order("uploaded_at desc").Find(&firmwares).Error; err != nil {
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{
			Success: false, 
			Message: "Gagal mengambil daftar firmware dari database",
		})
		return
	}

	// Kembalikan response JSON yang berisi array objek firmware
	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Data:    firmwares,
	})
}