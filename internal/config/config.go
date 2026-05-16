package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Control ControlConfig `yaml:"control"`
	Gateway GatewayConfig `yaml:"gateway"`
	Routes  []RouteConfig `yaml:"routes"`
	Log     LogConfig     `yaml:"log"`
}

type ControlConfig struct {
	Host              string `yaml:"host"`
	Port              int    `yaml:"port"`
	HeartbeatInterval int    `yaml:"heartbeat_interval"`
	HeartbeatTimeout  int    `yaml:"heartbeat_timeout"`
}

type GatewayConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	ReadTimeout  int    `yaml:"read_timeout"`
	WriteTimeout int    `yaml:"write_timeout"`
	IdleTimeout  int    `yaml:"idle_timeout"`
}

type RouteConfig struct {
	Host       string `yaml:"host"`
	DeviceID   string `yaml:"device_id"`
	TargetPort int    `yaml:"target_port"`
}

type LogConfig struct {
	Level    string `yaml:"level"`
	Encoding string `yaml:"encoding"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
