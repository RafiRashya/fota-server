package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"fota-backend/internal/middleware"
	"fota-backend/internal/models"
	"fota-backend/internal/mqtt"
	"io"
	"log"
	"net/http"
	"time"

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

func (h *FirmwareHandler) Upload(w http.ResponseWriter, r *http.Request){
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed!", http.StatusMethodNotAllowed)
		return
	}

	version := r.FormValue("version")
	nodeName := r.FormValue("node_name")
	if version == "" || nodeName == ""{
		http.Error(w, "Version and Node Label Parameter Cannot be Empty", http.StatusBadRequest)
		return
	}

	r.ParseMultipartForm(10 << 20)

	file, header, err := r.FormFile("firmware")
	if err != nil{
		http.Error(w, "Fail to parse file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	var node models.Node
	if err := h.Database.Where("name = ?", nodeName).First(&node).Error; err != nil{
		http.Error(w, "Can't Find Node", http.StatusNotFound)
		return
	}

	ctx := context.Background()
	objectKey := fmt.Sprintf("firmware/%s/v%s/nimble-shm-ota.bin", node.MacAddress, version)

	bucket := h.StorageClient.Bucket(h.BucketName)
	obj := bucket.Object(objectKey)
	writer := obj.NewWriter(ctx)

	if _, err := io.Copy(writer, file); err != nil{
		log.Printf("[GCS] Fail when copying file : %v", err)
		http.Error(w, "Fail Uploading firmware to Cloud Storage", http.StatusInternalServerError)
		return
	}
	if err := writer.Close(); err != nil{
		log.Printf("[GCS] Fail when closing writer : %v", err)
		http.Error(w, "Fail to finishing the Upload", http.StatusInternalServerError)
		return
	}

	opts := &storage.SignedURLOptions{
		GoogleAccessID:		h.GoogleAccessID,
		PrivateKey: 		h.PrivateKey,
		Method: 			http.MethodGet,
		Expires: 			time.Now().Add(15 * time.Minute),
	}

	signedURL, err := storage.SignedURL(h.BucketName, objectKey, opts)
	if err != nil{
		log.Printf("[GCS] Fail to create Signed URL : %v", err)
		http.Error(w, "Fail to create Signed URL", http.StatusInternalServerError)
		return
	}

	newFirmware := models.Firmware{
		Version:  version,
		FileName: header.Filename,
		FileSize: int(header.Size),
		Checksum: "sha256-terenkripsi", // Idealnya menggunakan fungsi hash sungguhan
	}
	h.Database.Create(&newFirmware)

	// 3. Cari Admin (Sementara ambil user pertama di DB sebagai pemicu)
	userIDStr, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		http.Error(w, "Fail to obtain Identity From Token", http.StatusUnauthorized)
		return
	}

	adminId, err := uuid.Parse(userIDStr)
	if err != nil{
		http.Error(w, "Invalid Admin Id Format", http.StatusBadGateway)
		return
	}

	// 4. Buat Tiket Log OTA dengan status PENDING
	otaLog := models.OtaLog{
		NodeID:           node.ID,
		TargetFirmwareID: newFirmware.ID,
		Status:           "PENDING",
		TriggeredBy:      adminId,
		StartedAt:        time.Now(),
	}
	h.Database.Create(&otaLog)

	triggerMsg := map[string]string{
		"cmd": "start_ota",
		"url": signedURL,
		"target_version": version,
	}

	payload, _ := json.Marshal(triggerMsg)
	if h.MQTTClient != nil {
		h.MQTTClient.Publish("shm/ota/trigger", 1, false, payload)
		log.Printf("[FOTA] Success triggering v%s update to Gateway!", version)
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Successfully upload %s Firmware file and publishing update trigger", header.Filename)
}