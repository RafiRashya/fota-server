package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"fota-backend/internal/api/handler"
	"fota-backend/internal/database"
	"fota-backend/internal/models"
	"fota-backend/internal/api/router"
	"fota-backend/internal/mqtt"

	"cloud.google.com/go/storage"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"gorm.io/gorm"
)

func main() {
	if err := godotenv.Load(); err != nil{
		log.Println("Failed to Load .env File, Using System Environment")
	}

	db := database.InitDB()
	log.Println("Database siap digunakan!")

	otaHandler := handler.NewOTAHandler(db)

	brokerAddr := os.Getenv("MQTT_BROKER") 
	clientID := os.Getenv("MQTT_CLIENT_ID")
	mqttUsername := os.Getenv("MQTT_USERNAME")
	mqttPassword := os.Getenv("MQTT_PASSWORD")
	
	// Panggil fungsi yang sudah diperbarui dengan 4 parameter
	mqttClient := mqtt.NewMQTTClient(brokerAddr, clientID, mqttUsername, mqttPassword)
	if err := mqttClient.Connect(); err != nil {
		log.Fatalf("Gagal inisialisasi MQTT TLS: %v", err)
	}
	defer mqttClient.Disconnect()

	err := mqttClient.Subscribe("shm/ota/status", 1, otaHandler.HandleStatusUpdate)
	if err != nil {
		log.Printf("Gagal subscribe ke topik status OTA: %v", err)
	}

	telemetryHandler := handler.NewTelemetryHandler(db)
	err = mqttClient.Subscribe("shm/telemetry", 0, telemetryHandler.HandleTelemetry)
	if err != nil{
		log.Printf("Gagal subscribe ke topik telemetry: %v", err)
	}

	ctx := context.Background()
	saFilePath := os.Getenv("GCS_SA_PATH")

	saKeyData, err := os.ReadFile(saFilePath)
	if err != nil{
		log.Fatalf("Fail to read Service Account: %v", err)
	}

	jwtConf, err := google.JWTConfigFromJSON(saKeyData)
	if err != nil{
		log.Fatalf("Fail when parsing Service Account JSON: %v", err)
	}

	gcsClient, err := storage.NewClient(ctx, option.WithAuthCredentialsJSON(option.ServiceAccount, saKeyData))
	if err != nil{
		log.Fatalf("Fail to create GCS Client : %v", err)
	}
	defer gcsClient.Close()

	bucketName := os.Getenv("GCS_BUCKET_NAME")

	// Cek apakah firmware 1.0.0 ada di database
	var defaultFw models.Firmware
	inDB := true
	err = db.Where("version = ?", "1.0.0").First(&defaultFw).Error
	if err == gorm.ErrRecordNotFound {
		inDB = false
	}

	// Cek apakah firmware 1.0.0 ada di GCS
	inGCS := true
	objectKey := "firmware/v1.0.0/nimble-shm-ota.bin"
	bucket := gcsClient.Bucket(bucketName)
	_, errAttr := bucket.Object(objectKey).Attrs(ctx)
	if errAttr != nil {
		inGCS = false
	}

	// Hanya jalankan seeder jika belum ada di database DAN belum ada di GCS
	if !inDB && !inGCS {
		database.SeedDefaultFirmware(db, gcsClient, bucketName)
	}

	// 2. Inisialisasi Handler & Router 
	authHandler := handler.NewAuthHandler(db)
	userHandler := handler.NewUserManagementHandler(db)
	deviceHandler := handler.NewDeviceHandler(db)
	dashboardHandler := handler.NewDashboardHandler(db)
	err = mqttClient.Subscribe("shm/gateway/status", 1, deviceHandler.HandleGatewayStatus)
	if err != nil {
		log.Printf("Gagal subscribe ke topik status gateway: %v", err)
	}
	
	fwHandler := handler.NewFirmwareHandler(
		gcsClient,
		bucketName,
		jwtConf.Email,
		jwtConf.PrivateKey,
		mqttClient,
		db,
	)
	go fwHandler.StartOTAWorker()
	mux := router.SetupRouter(fwHandler, authHandler, dashboardHandler, userHandler, deviceHandler)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			thresholdTime := time.Now().Add(-60 * time.Second)
			
			// 1. Cek Node yang terputus
			resultNode := db.Model(&models.Node{}).
				Where("status = ? AND updated_at < ?", "ONLINE", thresholdTime).
				Update("status", "OFFLINE")

			if resultNode.RowsAffected > 0 {
				log.Printf("[WATCHDOG] Mendeteksi %d Node kehilangan koneksi. Diubah ke OFFLINE.", resultNode.RowsAffected)
			}
		}
	}()

	// 3. Menjalankan Server
	port := os.Getenv("PORT")
	fmt.Printf("Backend FOTA Server berjalan di PORT %s\n", port)
	log.Fatal(http.ListenAndServe(port, mux))
}