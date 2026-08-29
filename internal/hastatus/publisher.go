package hastatus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"chatgpt-codex-proxy/internal/config"
)

const publishTimeout = 5 * time.Second

type Publisher struct {
	cfg       config.HomeAssistantConfig
	source    *Source
	logger    *slog.Logger
	client    mqtt.Client
	connectMu sync.Mutex
}

type discoveryDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
}

type availabilityConfig struct {
	Topic               string `json:"topic"`
	PayloadAvailable    string `json:"payload_available,omitempty"`
	PayloadNotAvailable string `json:"payload_not_available,omitempty"`
	ValueTemplate       string `json:"value_template,omitempty"`
}

type discoveryConfig struct {
	Name                   string               `json:"name"`
	UniqueID               string               `json:"unique_id"`
	DefaultEntityID        string               `json:"default_entity_id"`
	ObjectID               string               `json:"object_id"`
	StateTopic             string               `json:"state_topic"`
	ValueTemplate          string               `json:"value_template"`
	JSONAttributesTopic    string               `json:"json_attributes_topic,omitempty"`
	JSONAttributesTemplate string               `json:"json_attributes_template,omitempty"`
	AvailabilityTopic      string               `json:"availability_topic,omitempty"`
	Availability           []availabilityConfig `json:"availability,omitempty"`
	AvailabilityMode       string               `json:"availability_mode,omitempty"`
	PayloadAvailable       string               `json:"payload_available,omitempty"`
	PayloadNotAvailable    string               `json:"payload_not_available,omitempty"`
	UnitOfMeasurement      string               `json:"unit_of_measurement,omitempty"`
	StateClass             string               `json:"state_class,omitempty"`
	DeviceClass            string               `json:"device_class,omitempty"`
	Icon                   string               `json:"icon"`
	EntityCategory         string               `json:"entity_category,omitempty"`
	Device                 discoveryDevice      `json:"device"`
}

func NewPublisher(cfg config.HomeAssistantConfig, source *Source, logger *slog.Logger) *Publisher {
	p := &Publisher{cfg: cfg, source: source, logger: logger}
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.MQTTBroker).
		SetClientID(cfg.MQTTClientID).
		SetUsername(cfg.MQTTUsername).
		SetPassword(cfg.MQTTPassword).
		SetCleanSession(false).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(10*time.Second).
		SetConnectTimeout(publishTimeout).
		SetKeepAlive(30*time.Second).
		SetPingTimeout(5*time.Second).
		SetOrderMatters(false).
		SetWill(p.availabilityTopic(), "offline", 1, true)
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		logger.Warn("Home Assistant MQTT connection lost", "error", err)
	})
	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		go func() {
			if err := p.publishDiscovery(); err != nil {
				logger.Warn("Home Assistant MQTT discovery publish failed", "error", err)
			}
		}()
	})
	p.client = mqtt.NewClient(opts)
	return p
}

func (p *Publisher) Run(ctx context.Context) {
	if err := p.ensureConnected(); err != nil {
		p.logger.Warn("Home Assistant MQTT initial connection failed", "error", err)
	}
	defer p.close()
	p.publishSnapshot(ctx)
	ticker := time.NewTicker(p.cfg.PublishInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.publishSnapshot(ctx)
		}
	}
}

func (p *Publisher) publishSnapshot(ctx context.Context) {
	state := p.source.Snapshot(ctx)
	payload, err := json.Marshal(state)
	if err != nil {
		p.logger.Warn("Home Assistant status encoding failed", "error", err)
		return
	}
	if err := p.ensureConnected(); err != nil {
		p.logger.Warn("Home Assistant MQTT status connection failed", "error", err)
		return
	}
	if err := p.publish(p.stateTopic(), payload, true); err != nil {
		p.logger.Warn("Home Assistant MQTT status publish failed", "error", err)
		return
	}
	p.logger.Info("Home Assistant status published",
		"five_hour_remaining_percent", state.FiveHourRemainingPercent,
		"weekly_remaining_percent", state.WeeklyRemainingPercent,
		"available_reset_count", state.AvailableResetCount,
		"compression_active", state.CompressionActive,
		"quota_fresh", state.QuotaFresh,
	)
}

func (p *Publisher) publishDiscovery() error {
	device := discoveryDevice{
		Identifiers:  []string{"laegerfeld_codex_proxy"},
		Name:         "Codex-Proxy",
		Manufacturer: "Lägerfeld",
		Model:        "Codex Token Gateway",
	}
	entities := []struct {
		component string
		objectID  string
		config    discoveryConfig
	}{
		{component: "sensor", objectID: "codex_5_stunden_limit", config: discoveryConfig{
			Name: "Codex 5-Stunden-Limit", UniqueID: "codex_five_hour_limit_remaining", DefaultEntityID: "sensor.codex_5_stunden_limit", ObjectID: "codex_5_stunden_limit",
			StateTopic: p.stateTopic(), ValueTemplate: "{{ value_json.five_hour_remaining_percent }}",
			JSONAttributesTopic: p.stateTopic(), JSONAttributesTemplate: "{{ {'used_percent': value_json.five_hour_used_percent, 'reset_at': value_json.five_hour_reset_at, 'window_seconds': value_json.five_hour_window_seconds, 'fresh': value_json.quota_fresh, 'fetched_at': value_json.quota_fetched_at, 'updated_at': value_json.updated_at} | tojson }}",
			UnitOfMeasurement: "%", StateClass: "measurement", Icon: "mdi:timer-sand", EntityCategory: "diagnostic", Device: device,
			Availability: p.dataAvailability("five_hour_remaining_percent"), AvailabilityMode: "all",
		}},
		{component: "sensor", objectID: "codex_wochenlimit", config: discoveryConfig{
			Name: "Codex Wochenlimit", UniqueID: "codex_weekly_limit_remaining", DefaultEntityID: "sensor.codex_wochenlimit", ObjectID: "codex_wochenlimit",
			StateTopic: p.stateTopic(), ValueTemplate: "{{ value_json.weekly_remaining_percent }}",
			JSONAttributesTopic: p.stateTopic(), JSONAttributesTemplate: "{{ {'used_percent': value_json.weekly_used_percent, 'reset_at': value_json.weekly_reset_at, 'window_seconds': value_json.weekly_window_seconds, 'fresh': value_json.quota_fresh, 'fetched_at': value_json.quota_fetched_at, 'updated_at': value_json.updated_at} | tojson }}",
			UnitOfMeasurement: "%", StateClass: "measurement", Icon: "mdi:calendar-week", EntityCategory: "diagnostic", Device: device,
			Availability: p.dataAvailability("weekly_remaining_percent"), AvailabilityMode: "all",
		}},
		{component: "sensor", objectID: "codex_limit_resets", config: discoveryConfig{
			Name: "Codex verfügbare Limit-Resets", UniqueID: "codex_available_limit_resets", DefaultEntityID: "sensor.codex_limit_resets", ObjectID: "codex_limit_resets",
			StateTopic: p.stateTopic(), ValueTemplate: "{{ value_json.available_reset_count }}",
			JSONAttributesTopic: p.stateTopic(), JSONAttributesTemplate: "{{ {'expirations': value_json.reset_expirations, 'next_expiry': value_json.next_reset_expiry, 'details_complete': value_json.reset_details_complete, 'updated_at': value_json.updated_at} | tojson }}",
			Icon: "mdi:restore", EntityCategory: "diagnostic", Device: device,
			Availability: p.dataAvailability("available_reset_count"), AvailabilityMode: "all",
		}},
		{component: "binary_sensor", objectID: "codex_kompression_aktiv", config: discoveryConfig{
			Name: "Codex Kompression aktiv", UniqueID: "codex_compression_active", DefaultEntityID: "binary_sensor.codex_kompression_aktiv", ObjectID: "codex_kompression_aktiv",
			StateTopic: p.stateTopic(), ValueTemplate: "{{ 'ON' if value_json.compression_active else 'OFF' }}",
			JSONAttributesTopic: p.stateTopic(), JSONAttributesTemplate: "{{ {'configured': value_json.compression_configured, 'compressor_reachable': value_json.compressor_reachable, 'minimum_tokens': value_json.compression_min_tokens, 'target_ratio': value_json.compression_target_ratio, 'updated_at': value_json.updated_at} | tojson }}",
			AvailabilityTopic: p.availabilityTopic(), PayloadAvailable: "online", PayloadNotAvailable: "offline",
			DeviceClass: "running", Icon: "mdi:text-box-compress-outline", EntityCategory: "diagnostic", Device: device,
		}},
	}
	for _, entity := range entities {
		body, err := json.Marshal(entity.config)
		if err != nil {
			return fmt.Errorf("encode discovery %s: %w", entity.objectID, err)
		}
		topic := fmt.Sprintf("%s/%s/%s/config", strings.Trim(p.cfg.DiscoveryPrefix, "/"), entity.component, entity.objectID)
		if err := p.publish(topic, body, true); err != nil {
			return err
		}
	}
	return p.publish(p.availabilityTopic(), []byte("online"), true)
}

func (p *Publisher) dataAvailability(field string) []availabilityConfig {
	return []availabilityConfig{
		{Topic: p.availabilityTopic(), PayloadAvailable: "online", PayloadNotAvailable: "offline"},
		{Topic: p.stateTopic(), ValueTemplate: fmt.Sprintf("{{ 'online' if value_json.%s is not none else 'offline' }}", field)},
	}
}

func (p *Publisher) ensureConnected() error {
	if p.client.IsConnected() {
		return nil
	}
	p.connectMu.Lock()
	defer p.connectMu.Unlock()
	if p.client.IsConnected() {
		return nil
	}
	token := p.client.Connect()
	if !token.WaitTimeout(publishTimeout) || token.Error() != nil {
		return mqttTokenError("connect to MQTT", token)
	}
	return nil
}

func (p *Publisher) publish(topic string, payload []byte, retained bool) error {
	token := p.client.Publish(topic, 1, retained, payload)
	if !token.WaitTimeout(publishTimeout) || token.Error() != nil {
		return mqttTokenError("publish MQTT topic "+topic, token)
	}
	return nil
}

func (p *Publisher) close() {
	if p.client == nil || !p.client.IsConnected() {
		return
	}
	_ = p.publish(p.availabilityTopic(), []byte("offline"), true)
	p.client.Disconnect(250)
}

func (p *Publisher) stateTopic() string { return strings.Trim(p.cfg.MQTTBaseTopic, "/") + "/status" }
func (p *Publisher) availabilityTopic() string {
	return strings.Trim(p.cfg.MQTTBaseTopic, "/") + "/availability"
}

func mqttTokenError(operation string, token mqtt.Token) error {
	if token.Error() != nil {
		return fmt.Errorf("%s: %w", operation, token.Error())
	}
	return fmt.Errorf("%s: timeout", operation)
}
