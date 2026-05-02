package handler

import (
	"encoding/json"
	"log"
	"time"

	"fota-backend/internal/models"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"gorm.io/gorm"
)

// OTAStatusPayload merepresentasikan struktur JSON dari Gateway
type OTAStatusPayload struct {
	NodeMAC string `json:"node_mac"`
	Status  string `json:"status"`
}

type OTAHandler struct {
	DB *gorm.DB
}

func NewOTAHandler(db *gorm.DB) *OTAHandler {
	return &OTAHandler{
		DB: db,
	}
}

func (h *OTAHandler) HandleStatusUpdate(client pahomqtt.Client, msg pahomqtt.Message) {
	var payload OTAStatusPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Printf("[OTA-HANDLER] Gagal decode pesan: %v", err)
		return
	}

	var node models.Node
	if err := h.DB.Where("mac_address = ?", payload.NodeMAC).First(&node).Error; err != nil {
		log.Printf("[OTA-HANDLER] Node dengan MAC %s tidak ditemukan", payload.NodeMAC)
		return
	}

	// 1. Cari log OTA terbaru untuk node ini (berdasarkan waktu mulai)
	var lastLog models.OtaLog
	err := h.DB.Where("node_id = ?", node.ID).Order("started_at desc").First(&lastLog).Error
	if err != nil {
		log.Printf("[OTA-HANDLER] Belum ada tiket OTA untuk Node %s", payload.NodeMAC)
		return
	}

	// 2. Update status
	h.DB.Model(&lastLog).Update("status", payload.Status)
	log.Printf("[DB] Status OTA Node %s diupdate menjadi: %s", payload.NodeMAC, payload.Status)

	// 3. JIKA SUKSES MUTLAK: Catat waktu selesai & Update versi
	if payload.Status == "SUCCESS" {
		h.DB.Model(&lastLog).Update("completed_at", time.Now())
		
		h.DB.Model(&node).Update("current_firmware_id", lastLog.TargetFirmwareID)
		log.Printf("[DB] Selesai! Node %s resmi berjalan di versi baru.", payload.NodeMAC)
	}
}