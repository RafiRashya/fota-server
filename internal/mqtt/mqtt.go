package mqtt

import (
	"crypto/tls"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTClient membungkus klien paho mqtt
type MQTTClient struct {
	client mqtt.Client
}

// NewMQTTClient menginisialisasi konfigurasi klien MQTT
func NewMQTTClient(broker , clientID, username, password string) *MQTTClient {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID(clientID)
	opts.SetUsername(username)
	opts.SetPassword(password)
	opts.SetAutoReconnect(true)
	opts.SetConnectTimeout(10 * time.Second)

	tlsconfig := &tls.Config{
		InsecureSkipVerify: false,
	}
	opts.SetTLSConfig(tlsconfig)

	// Callback saat koneksi berhasil
	opts.OnConnect = func(c mqtt.Client) {
		log.Printf("Berhasil terhubung ke MQTT Broker: %s", broker)
	}

	// Callback saat koneksi terputus
	opts.OnConnectionLost = func(c mqtt.Client, err error) {
		log.Printf("Koneksi ke MQTT Broker terputus: %v", err)
	}

	client := mqtt.NewClient(opts)
	return &MQTTClient{client: client}
}

// Connect melakukan koneksi fisik ke broker
func (m *MQTTClient) Connect() error {
	if token := m.client.Connect(); token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

// Publish mengirim pesan ke topik tertentu
func (m *MQTTClient) Publish(topic string, qos byte, retained bool, payload interface{}) error {
	token := m.client.Publish(topic, qos, retained, payload)
	token.Wait()
	return token.Error()
}

// Disconnect memutuskan koneksi (digunakan saat server mati)
func (m *MQTTClient) Disconnect() {
	m.client.Disconnect(250)
}