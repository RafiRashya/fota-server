package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"fota-backend/internal/models"

	"cloud.google.com/go/storage"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDB() *gorm.DB {
	if err := godotenv.Load(); err != nil{
		log.Println("Failed to Load .env File, Using System Environment")
	}
	
	host := os.Getenv("POSTGRES_HOST")
	user := os.Getenv("POSTGRES_USER")
	password := os.Getenv("POSTGRES_PASSWORD")
	dbname := os.Getenv("POSTGRES_DB")
	port := os.Getenv("POSTGRES_PORT")

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Jakarta",
		host, user, password, dbname, port)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info), // Menampilkan query SQL di terminal
	})
	if err != nil {
		log.Fatalf("Gagal terhubung ke Database: %v", err)
	}

	log.Println("Berhasil terhubung ke PostgreSQL di GCP!")

	// GORM Auto Migrate
	// Ini akan otomatis menerjemahkan struct Go menjadi tabel SQL yang persis dengan rancangan DDL Anda
	err = db.AutoMigrate(
		&models.User{},
		&models.Gateway{},
		&models.Firmware{},
		&models.Node{},
		&models.OtaLog{},
		&models.ShmTelemetry{},
		&models.RefreshToken{},
	)
	
	if err != nil {
		log.Fatalf("Gagal melakukan migrasi database: %v", err)
	}

	// KHUSUS TIMESCALEDB: Eksekusi query mentah untuk mengubah tabel shm_telemetry menjadi Hypertable
	// GORM tidak memiliki fungsi bawaan untuk ini, jadi kita gunakan db.Exec()
	db.Exec("SELECT create_hypertable('shm_telemetries', 'time', if_not_exists => TRUE);")

	return db
}

// SeedDefaultFirmware reads the local default binary file for 1.0.0, calculates
// its size and checksum, uploads it to GCS if missing, and inserts it into database.
func SeedDefaultFirmware(db *gorm.DB, gcsClient *storage.Client, bucketName string) {
	const defaultVersion = "1.0.0"
	
	// 1. Buka file .bin lokal
	localFilePath := filepath.Join("firmware_defaults", "nimble-shm-ota.bin")
	file, err := os.Open(localFilePath)
	if err != nil {
		log.Printf("[SEEDER] Warning: File default firmware tidak ditemukan di %s. Seeding dibatalkan. Error: %v", localFilePath, err)
		return
	}
	defer file.Close()

	// 2. Baca ukuran file (file size)
	stat, err := file.Stat()
	if err != nil {
		log.Printf("[SEEDER] Gagal membaca metadata file: %v", err)
		return
	}
	fileSize := int(stat.Size())

	// 3. Hitung Checksum SHA-256 secara dinamis
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		log.Printf("[SEEDER] Gagal menghitung checksum file: %v", err)
		return
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))

	// Reset pointer pembacaan file ke awal setelah hashing selesai
	file.Seek(0, io.SeekStart)

	// 4. Unggah file default .bin ke Google Cloud Storage
	objectKey := fmt.Sprintf("firmware/v%s/nimble-shm-ota.bin", defaultVersion)
	ctx := context.Background()
	bucket := gcsClient.Bucket(bucketName)
	obj := bucket.Object(objectKey)

	// Cek apakah file sudah terunggah di GCS untuk menghindari upload ulang yang tidak perlu
	_, err = obj.Attrs(ctx)
	if err != nil {
		// Jika belum ada di GCS, unggah sekarang
		writer := obj.NewWriter(ctx)
		if _, err := io.Copy(writer, file); err != nil {
			log.Printf("[SEEDER] Gagal mengunggah file default ke GCS: %v", err)
			return
		}
		if err := writer.Close(); err != nil {
			log.Printf("[SEEDER] Gagal menutup GCS writer: %v", err)
			return
		}
		log.Printf("[SEEDER] File default_v1.0.0.bin berhasil diunggah ke GCS: %s", objectKey)
	}

	// 5. Simpan Metadata ke Database PostgreSQL
	notes := "Default baseline stable firmware"
	newFirmware := models.Firmware{
		Version:      defaultVersion,
		FileName:     "nimble-shm-ota.bin",
		FileSize:     fileSize,
		Checksum:     checksum,
		ReleaseNotes: &notes,
		UploadedAt:   time.Now(),
	}

	if err := db.Create(&newFirmware).Error; err != nil {
		log.Printf("[SEEDER] Gagal menyimpan metadata firmware ke database: %v", err)
		return
	}

	log.Printf("[SEEDER] Sukses melakukan seeding firmware 1.0.0 di DB (Ukuran: %d bytes, Checksum: %s)", fileSize, checksum)
}