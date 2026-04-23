package handler

import (
	"encoding/json"
	"fmt"
	"fota-backend/internal/mqtt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

type FirmwareHandler struct {
	FirmwareDir string
	mqtt	*mqtt.MQTTClient
}

func NewFirmwareHandler(dir string, m *mqtt.MQTTClient) *FirmwareHandler {
	return &FirmwareHandler{
		FirmwareDir: dir,
		mqtt: m,
	}
}

func (h *FirmwareHandler) Download(w http.ResponseWriter, r *http.Request){
	if r.Method != http.MethodGet{
		http.Error(w, "Method Not Allowed!", http.StatusMethodNotAllowed)
		return
	}

	version := r.URL.Query().Get("version")
	if version == ""{
		http.Error(w, "Version Parameter Cannot be Empty", http.StatusBadRequest)
		return
	}

	fileName := fmt.Sprintf("firmware_v%s.bin", version)
	filePath := filepath.Join(h.FirmwareDir, fileName)

	fileInfo, err := os.Stat(filePath)
	if os.IsNotExist(err){
		http.Error(w, "Could Not Find Firmware on server", http.StatusNotFound)
		return
	}

	cleanPath := filepath.Clean(filePath)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

	http.ServeFile(w, r, cleanPath)

	log.Printf("Successfully Download: %s (%d)", fileName, fileInfo.Size())
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

	fileName := fmt.Sprintf("firmware_v%s.bin", version)
	filePath := filepath.Join(h.FirmwareDir, fileName)
	dst, err := os.Create(filePath)
	if err != nil{
		http.Error(w, "Couldn't Save File", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err !=nil{
		http.Error(w, "Fail while writing Firmware file", http.StatusInternalServerError)
		return
	}

	log.Printf("[SERVER] Success Uploading %s File", header.Filename)

	serverIP := "172.16.18.109"
	downloadURL := fmt.Sprintf("http://%s:5000/api/v1/firmware/download?version=%s", serverIP, version)
	triggerMsg := map[string]string{
		"cmd": "start_ota",
		"url": downloadURL,
	}
	payload, _ := json.Marshal(triggerMsg)

	err = h.mqtt.Publish("shm/ota/trigger", 1, false, payload)
	if err != nil{
		log.Printf("[MQTT] Trigger failed to publish: %v", err)
	}else{
		log.Printf("[MQTT] Update trigger successfully sended to shm/ota/trigger topic")
	}

	w.WriteHeader(http.StatusCreated)
	fmt.Fprintf(w, "Successfully upload %s Firmware file and publishing update trigger", header.Filename)
}