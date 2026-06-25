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
	"log"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// Struct untuk request bulk OTA trigger
type TriggerBulkOTAReq struct {
	NodeIDs    []string `json:"node_ids"`
	FirmwareID string   `json:"firmware_id"`
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

	// 1. ParseMultipartForm (Batas 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "Gagal memproses form data atau ukuran file terlalu besar",
		})
		return
	}

	version := r.FormValue("version")
	releaseNotes := r.FormValue("release_notes")

	if version == "" {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{
			Success: false,
			Message: "Version parameter cannot be empty",
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

	// 2. Simpan data Firmware ke Database
	newFirmware := models.Firmware{
		Version:      version,
		FileName:     header.Filename,
		FileSize:     int(header.Size),
		Checksum:     calculatedChecksum,
		ReleaseNotes: releaseNotesPtr,
	}
	if err := h.Database.Create(&newFirmware).Error; err != nil {
		middleware.WriteJSON(w, http.StatusConflict, middleware.JsonResponse{
			Success: false,
			Message: "Gagal menyimpan data firmware. Pastikan versi belum pernah digunakan.",
		})
		return
	}

	middleware.WriteJSON(w, http.StatusCreated, middleware.JsonResponse{
		Success: true,
		Message: fmt.Sprintf("Firmware versi %s (%s) berhasil disimpan ke sistem", version, header.Filename),
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

	// 3. Ambil Admin ID dari JWT Token
	userIDStr, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		middleware.WriteJSON(w, http.StatusUnauthorized, middleware.JsonResponse{Success: false, Message: "Unauthorized"})
		return
	}
	adminId, _ := uuid.Parse(userIDStr)

	// 4. Buat tiket/log OTA baru berstatus PENDING
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

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Message: fmt.Sprintf("Berhasil mendaftarkan antrean OTA versi %s untuk Node %s", firmware.Version, node.Name),
	})
}

// TriggerBulkOTA places multiple OTA requests into the database queue (status PENDING)
func (h *FirmwareHandler) TriggerBulkOTA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{Success: false, Message: "Method Not Allowed"})
		return
	}

	var req TriggerBulkOTAReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "Format request tidak valid"})
		return
	}

	if len(req.NodeIDs) == 0 {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "node_ids parameter cannot be empty"})
		return
	}

	// 1. Cari data Firmware yang sudah ada di DB
	var firmware models.Firmware
	if err := h.Database.First(&firmware, "id = ?", req.FirmwareID).Error; err != nil {
		middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{Success: false, Message: "Firmware tidak ditemukan di sistem"})
		return
	}

	// 2. Ambil Admin ID dari JWT Token
	userIDStr, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		middleware.WriteJSON(w, http.StatusUnauthorized, middleware.JsonResponse{Success: false, Message: "Unauthorized"})
		return
	}
	adminId, _ := uuid.Parse(userIDStr)

	// 3. Cari all nodes
	var nodes []models.Node
	if err := h.Database.Where("id IN ?", req.NodeIDs).Find(&nodes).Error; err != nil {
		middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{Success: false, Message: "Gagal mengambil data node"})
		return
	}

	if len(nodes) == 0 {
		middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{Success: false, Message: "Tidak ada node valid yang ditemukan"})
		return
	}

	// 4. Daftarkan log OTA PENDING dalam database transaction untuk keamanan data
	tx := h.Database.Begin()
	var queuedCount int
	for _, node := range nodes {
		otaLog := models.OtaLog{
			NodeID:           node.ID,
			TargetFirmwareID: firmware.ID,
			Status:           "PENDING",
			TriggeredBy:      adminId,
			StartedAt:        time.Now(),
		}
		if err := tx.Create(&otaLog).Error; err != nil {
			tx.Rollback()
			middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal mendaftarkan antrean bulk OTA"})
			return
		}
		queuedCount++
	}
	tx.Commit()

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Message: fmt.Sprintf("Berhasil mendaftarkan %d node ke antrean OTA versi %s", queuedCount, firmware.Version),
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

// StartOTAWorker runs a background goroutine to process OTA queues sequentially
func (h *FirmwareHandler) StartOTAWorker() {
	log.Println("[OTA-WORKER] Background worker started")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Jalankan proses penarikan antrean di dalam transaksi DB
		tx := h.Database.Begin()

		// 1. Ambil antrean PENDING tertua dan berikan pessimistic lock (SELECT ... FOR UPDATE)
		// pada baris ini agar instans backend lain tidak bisa mengakses/mengubah baris yang sama.
		var otaLog models.OtaLog
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Preload("Node").Preload("TargetFirmware").
			Where("status = ?", "PENDING").
			Order("started_at asc").
			First(&otaLog).Error

		if err != nil {
			tx.Rollback()
			if err == gorm.ErrRecordNotFound {
				continue
			}
			log.Printf("[OTA-WORKER] Gagal mengambil antrean: %v", err)
			continue
		}

		// 2. Periksa apakah saat ini ada proses OTA lain yang sedang aktif (IN_PROGRESS) secara global.
		// Karena kita memegang lock pada baris PENDING tertua tadi, kita dijamin aman dari race condition.
		var activeCount int64
		if err := tx.Model(&models.OtaLog{}).Where("status = ?", "IN_PROGRESS").Count(&activeCount).Error; err != nil {
			log.Printf("[OTA-WORKER] Gagal memeriksa status IN_PROGRESS: %v", err)
			tx.Rollback()
			continue
		}

		if activeCount > 0 {
			// Jika ada proses OTA lain yang sedang berjalan, batalkan transaksi agar lock dilepaskan
			// dan lewati iterasi ini (proses antrean PENDING ini ditunda secara sekuensial).
			tx.Rollback()
			continue
		}

		// 3. Update status antrean ini ke IN_PROGRESS
		otaLog.Status = "IN_PROGRESS"
		otaLog.StartedAt = time.Now()
		if err := tx.Save(&otaLog).Error; err != nil {
			log.Printf("[OTA-WORKER] Gagal mengupdate status ke IN_PROGRESS: %v", err)
			tx.Rollback()
			continue
		}

		// Commit transaksi untuk menyimpan status IN_PROGRESS dan melepas lock
		if err := tx.Commit().Error; err != nil {
			log.Printf("[OTA-WORKER] Gagal commit transaksi OTA: %v", err)
			continue
		}

		log.Printf("[OTA-WORKER] Memproses antrean OTA untuk Node %s (%s) ke versi %s", otaLog.Node.Name, otaLog.Node.MacAddress, otaLog.TargetFirmware.Version)

		// 2. Generate signed URL
		objectKey := fmt.Sprintf("firmware/v%s/nimble-shm-ota.bin", otaLog.TargetFirmware.Version)
		opts := &storage.SignedURLOptions{
			GoogleAccessID: h.GoogleAccessID,
			PrivateKey:     h.PrivateKey,
			Method:         "GET",
			Expires:        time.Now().Add(30 * time.Minute), // Berlaku 24 Jam untuk keamanan selama antrean panjang
		}

		signedURL, err := storage.SignedURL(h.BucketName, objectKey, opts)
		if err != nil {
			log.Printf("[OTA-WORKER] Gagal membuat signed URL: %v", err)
			h.Database.Model(&otaLog).Update("status", "FAILED")
			continue
		}

		// 3. Publish trigger ke MQTT
		triggerMsg := map[string]string{
			"cmd":            "start_ota",
			"target_mac":     otaLog.Node.MacAddress,
			"url":            signedURL,
			"target_version": otaLog.TargetFirmware.Version,
		}

		payload, err := json.Marshal(triggerMsg)
		if err != nil {
			log.Printf("[OTA-WORKER] Gagal marshal payload: %v", err)
			h.Database.Model(&otaLog).Update("status", "FAILED")
			continue
		}

		if h.MQTTClient == nil {
			log.Printf("[OTA-WORKER] Klien MQTT nil, gagal memicu OTA")
			h.Database.Model(&otaLog).Update("status", "FAILED")
			continue
		}

		if err := h.MQTTClient.Publish("shm/ota/trigger", 1, false, payload); err != nil {
			log.Printf("[OTA-WORKER] Gagal mempublikasikan payload OTA: %v", err)
			h.Database.Model(&otaLog).Update("status", "FAILED")
			continue
		}

		log.Printf("[OTA-WORKER] Trigger OTA berhasil dipublish untuk Node %s. Menunggu respon...", otaLog.Node.MacAddress)

		// 4. Blokir dan tunggu respon sukses/gagal atau timeout (3 menit)
		timeout := time.After(5 * time.Minute)
		completed := false

		for !completed {
			select {
			case mac := <-OTAUpdateChan:
				if mac == otaLog.Node.MacAddress {
					log.Printf("[OTA-WORKER] Node %s selesai memproses update", mac)
					completed = true
				} else {
					log.Printf("[OTA-WORKER] Mengabaikan status update node lain: %s", mac)
				}
			case <-timeout:
				log.Printf("[OTA-WORKER] Timeout 5 menit terlampaui untuk node %s (%s). Update status menjadi FAILED.", otaLog.Node.Name, otaLog.Node.MacAddress)
				
				// Cek terlebih dahulu di DB, pastikan status belum diubah oleh handleStatusUpdate di detik-detik terakhir
				var checkLog models.OtaLog
				if err := h.Database.First(&checkLog, "id = ?", otaLog.ID).Error; err == nil {
					if checkLog.Status == "IN_PROGRESS" || checkLog.Status == "PENDING" {
						h.Database.Model(&checkLog).Update("status", "FAILED")
					}
				}
				completed = true
			}
		}
	}
}

// DeleteFirmware deletes a firmware from DB (updating nodes and deleting logs) and also removes it from GCS
func (h *FirmwareHandler) DeleteFirmware(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		middleware.WriteJSON(w, http.StatusMethodNotAllowed, middleware.JsonResponse{Success: false, Message: "Method Not Allowed"})
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		middleware.WriteJSON(w, http.StatusBadRequest, middleware.JsonResponse{Success: false, Message: "ID Firmware wajib disertakan"})
		return
	}

	// 1. Cari data Firmware
	var firmware models.Firmware
	if err := h.Database.First(&firmware, "id = ?", id).Error; err != nil {
		middleware.WriteJSON(w, http.StatusNotFound, middleware.JsonResponse{Success: false, Message: "Firmware tidak ditemukan"})
		return
	}

	// 2. Jalankan DB transaction untuk menghapus dependensi
	tx := h.Database.Begin()
	
	// Set CurrentFirmwareID ke NULL pada semua Node yang merujuk firmware ini
	if err := tx.Model(&models.Node{}).Where("current_firmware_id = ?", id).Update("current_firmware_id", nil).Error; err != nil {
		tx.Rollback()
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal melepaskan relasi Node"})
		return
	}

	// Hapus all OtaLogs yang merujuk ke firmware ini
	if err := tx.Where("target_firmware_id = ?", id).Delete(&models.OtaLog{}).Error; err != nil {
		tx.Rollback()
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal menghapus log OTA terkait"})
		return
	}

	// Hapus record Firmware dari DB
	if err := tx.Delete(&models.Firmware{}, "id = ?", id).Error; err != nil {
		tx.Rollback()
		middleware.WriteJSON(w, http.StatusInternalServerError, middleware.JsonResponse{Success: false, Message: "Gagal menghapus data firmware dari database"})
		return
	}

	tx.Commit()

	// 3. Hapus file biner firmware dari Google Cloud Storage
	ctx := context.Background()
	objectKey := fmt.Sprintf("firmware/v%s/nimble-shm-ota.bin", firmware.Version)
	bucket := h.StorageClient.Bucket(h.BucketName)
	obj := bucket.Object(objectKey)
	
	// Kita abaikan jika file tidak ditemukan di GCS (mungkin sudah dihapus manual)
	if err := obj.Delete(ctx); err != nil && err != storage.ErrObjectNotExist {
		log.Printf("[FIRMWARE-HANDLER] Gagal menghapus file dari GCS: %v", err)
	}

	middleware.WriteJSON(w, http.StatusOK, middleware.JsonResponse{
		Success: true,
		Message: fmt.Sprintf("Firmware versi %s berhasil dihapus dari sistem", firmware.Version),
	})
}