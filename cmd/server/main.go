package main

import (
	"fmt"
	"log"
	"net/http"
	
	"fota-backend/internal/api/handler"
	"fota-backend/internal/api/router"
	"fota-backend/internal/mqtt"
)

func main() {
	// 1. Konfigurasi MQTTS EMQX Cloud
	// Gunakan prefix 'ssl://' atau 'tls://' untuk port 8883
	brokerAddr := "ssl://f61f146a.ala.asia-southeast1.emqxsl.com:8883" 
	clientID := "fota_backend_windows"
	mqttUsername := "rafirashya" // Ganti dengan username EMQX Anda
	mqttPassword := "broker123" // Ganti dengan password EMQX Anda
	
	// Panggil fungsi yang sudah diperbarui dengan 4 parameter
	mqttClient := mqtt.NewMQTTClient(brokerAddr, clientID, mqttUsername, mqttPassword)
	if err := mqttClient.Connect(); err != nil {
		log.Fatalf("Gagal inisialisasi MQTT TLS: %v", err)
	}
	defer mqttClient.Disconnect()

	// 2. Inisialisasi Handler & Router 
	firmwareDir := "./firmware"
	fwHandler := handler.NewFirmwareHandler(firmwareDir, mqttClient)
	mux := router.SetupRouter(fwHandler)

	// 3. Menjalankan Server
	port := ":5000" // Mengubah ke port 5000 agar sesuai dengan URL OTA di Gateway ESP32 Anda
	fmt.Printf("Backend FOTA Server berjalan di http://localhost%s\n", port)
	log.Fatal(http.ListenAndServe(port, mux))
}