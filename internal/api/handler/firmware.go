package handler

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

type FirmwareHandler struct {
	FirmwareDir string
}

func NewFirmwareHandler(dir string) *FirmwareHandler {
	return &FirmwareHandler{
		FirmwareDir: dir,
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