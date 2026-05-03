package database

import (
	"log"
	"fota-backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"github.com/joho/godotenv"
	"os"
	"fmt"
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