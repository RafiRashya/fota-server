package handler

import (
	"encoding/json"
	"log"
	"time"

	"fota-backend/internal/models"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"gorm.io/gorm"
)

// Struktur JSON yang dikirim oleh Gateway ESP32-S3
type TelemetryPayload struct {
	NodeMAC string  `json:"node_mac"`
	Ax      float64 `json:"ax"`
	Ay      float64 `json:"ay"`
	Az      float64 `json:"az"`
	Vbatt   float64 `json:"vbatt"`
}

type TelemetryHandler struct {
	DB *gorm.DB
}

func NewTelemetryHandler(db *gorm.DB) *TelemetryHandler {
	return &TelemetryHandler{DB: db}
}

// HandleTelemetry adalah fungsi callback untuk topik MQTT "shm/telemetry"
func (h *TelemetryHandler) HandleTelemetry(client pahomqtt.Client, msg pahomqtt.Message) {
	var payload TelemetryPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		log.Printf("[TELEMETRY] Gagal parse JSON: %v", err)
		return
	}

	// 1. Cari Node ID berdasarkan MAC Address
	var node models.Node
	if err := h.DB.Where("mac_address = ?", payload.NodeMAC).First(&node).Error; err != nil {
		// Log ini di-comment atau diubah ke Debug jika tidak ingin terminal penuh
		// log.Printf("[TELEMETRY] Mengabaikan data: Node %s tidak terdaftar", payload.NodeMAC)
		return
	}

	currentTime := time.Now()

	// 2. Buat Record Data Getaran (TimescaleDB Hypertable)
	telemetryData := models.ShmTelemetry{
		Time:   currentTime,
		NodeID: node.ID,
		Ax:     payload.Ax,
		Ay:     payload.Ay,
		Az:     payload.Az,
		Vbatt:  payload.Vbatt,
	}

	// Insert data ke database
	if err := h.DB.Create(&telemetryData).Error; err != nil {
		log.Printf("[TELEMETRY] Gagal menyimpan data sensor: %v", err)
		return
	}

	// 3. Update status Node sebagai "Heartbeat" (Tanda Kehidupan)
	// Memperbarui baterai terakhir dan menandakan node sedang ONLINE
	h.DB.Model(&node).Updates(map[string]interface{}{
		"last_vbatt": payload.Vbatt,
		"status":     "ONLINE",
		"updated_at": currentTime,
	})

	// Opsional: Print ke terminal agar Anda bisa melihat aliran datanya
	log.Printf("[SHM-DB] Inserted Ax:%.2f Ay:%.2f Az:%.2f dari Node %s", payload.Ax, payload.Ay, payload.Az, payload.NodeMAC)
}