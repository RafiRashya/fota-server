package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"fota-backend/internal/api/handler"
	"fota-backend/internal/database"
	"fota-backend/internal/api/router"
	"fota-backend/internal/mqtt"

	"cloud.google.com/go/storage"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
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

	// 2. Inisialisasi Handler & Router 
	authHandler := handler.NewAuthHandler(db)
	
	fwHandler := handler.NewFirmwareHandler(
		gcsClient,
		bucketName,
		jwtConf.Email,
		jwtConf.PrivateKey,
		mqttClient,
		db,
	)
	dashboardHandler := handler.NewDashboardHandler(db)
	mux := router.SetupRouter(fwHandler, authHandler, dashboardHandler)

	// 3. Menjalankan Server
	port := os.Getenv("PORT")
	fmt.Printf("Backend FOTA Server berjalan di PORT %s\n", port)
	log.Fatal(http.ListenAndServe(port, mux))
}