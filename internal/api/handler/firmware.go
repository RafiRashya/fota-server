package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"fota-backend/internal/mqtt"
	"io"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/storage"
)

type FirmwareHandler struct {
	StorageClient	*storage.Client
	BucketName	string
	GoogleAccessID	string
	PrivateKey	[]byte
	MQTTClient	*mqtt.MQTTClient
}

func NewFirmwareHandler(client *storage.Client, bucket string, accessID string, privKey []byte, mqtt *mqtt.MQTTClient) *FirmwareHandler {
	return &FirmwareHandler{
		StorageClient: client,
		BucketName: bucket,
		GoogleAccessID: accessID,
		PrivateKey: privKey,
		MQTTClient: mqtt,
	}
}

func (h *FirmwareHandler) Upload(w http.ResponseWriter, r *http.Request){
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed!", http.StatusMethodNotAllowed)
		return
	}

	version := r.FormValue("version")
	if version == "" {
		http.Error(w, "Version Parameter Cannot be Empty", http.StatusBadRequest)
		return
	}

	r.ParseMultipartForm(10 << 20)

	file, header, err := r.FormFile("firmware")
	if err != nil{
		http.Error(w, "Fail to parse file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	ctx := context.Background()
	objectKey := fmt.Sprintf("firmware/v%s/nimble-shm-ota.bin", version)

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
		Expires: 			time.Now().Add(30 * time.Minute),
	}

	signedURL, err := storage.SignedURL(h.BucketName, objectKey, opts)
	if err != nil{
		log.Printf("[GCS] Fail to create Signed URL : %v", err)
		http.Error(w, "Fail to create Signed URL", http.StatusInternalServerError)
		return
	}

	triggerMsg := map[string]string{
		"cmd": "start_ota",
		"url": signedURL,
	}

	payload, _ := json.Marshal(triggerMsg)
	if h.MQTTClient != nil {
		h.MQTTClient.Publish("shm/ota/trigger", 1, false, payload)
		log.Printf("[FOTA] Success triggering v%s update to Gateway!", version)
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Successfully upload %s Firmware file and publishing update trigger", header.Filename)
}