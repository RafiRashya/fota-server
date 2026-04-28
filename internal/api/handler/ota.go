package handler

import (
	"gorm.io/gorm" // <--- Tambahkan import GORM
	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

type OTAHandler struct {
	DB *gorm.DB // <--- Wadah untuk koneksi database
}

// Tambahkan parameter *gorm.DB saat handler dibuat
func NewOTAHandler(db *gorm.DB) *OTAHandler {
	return &OTAHandler{
		DB: db,
	}
}

// Contoh penggunaan di dalam fungsi:
func (h *OTAHandler) HandleStatusUpdate(client pahomqtt.Client, msg pahomqtt.Message) {
    // ... parsing JSON ...
    
    // Sekarang Anda bisa memanggil h.DB untuk melakukan query!
    // Contoh: h.DB.Model(&models.OtaLog{}).Update(...)
}